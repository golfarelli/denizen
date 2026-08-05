package flow

import (
	"bytes"
	"crypto/rand"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/tus/tusd/v2/pkg/handler"

	"github.com/golfarelli/denizen/internal/storage"
)

// tusRequest issues a tus-protocol request as user, with the standard
// Tus-Resumable header every tus request needs (the whole /api/v1/uploads
// subtree also sits behind auth — see internal/router — so the bearer token
// is required here too, not just on the regular JSON endpoints).
func tusRequest(t *testing.T, method, url string, user registeredUser, body io.Reader, headers map[string]string) *http.Response {
	t.Helper()

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, url, err)
	}
	req.Header.Set("Tus-Resumable", "1.0.0")
	req.Header.Set("Authorization", "Bearer "+user.accessToken)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	return res
}

// uploadFile drives the full tus create+upload dance in one call and
// returns the resulting item — shared by tests (download, shares, quota)
// that need a real uploaded file to work with but aren't themselves testing
// the upload mechanics.
func uploadFile(t *testing.T, ts *testServer, user registeredUser, parentID *string, filename string, content []byte) apiItem {
	t.Helper()

	meta := map[string]string{"filename": filename}
	if parentID != nil {
		meta["parent_id"] = *parentID
	}

	createRes := tusRequest(t, http.MethodPost, ts.URL+"/api/v1/uploads/", user, nil, map[string]string{
		"Upload-Length":   strconv.Itoa(len(content)),
		"Upload-Metadata": handler.SerializeMetadataHeader(meta),
	})
	if createRes.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(createRes.Body)
		t.Fatalf("create upload for %q: got status %d, body: %s", filename, createRes.StatusCode, body)
	}
	location := createRes.Header.Get("Location")

	patchRes := tusRequest(t, http.MethodPatch, location, user, bytes.NewReader(content), map[string]string{
		"Content-Type":  "application/offset+octet-stream",
		"Upload-Offset": "0",
	})
	if patchRes.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(patchRes.Body)
		t.Fatalf("upload bytes for %q: got status %d, body: %s", filename, patchRes.StatusCode, body)
	}
	itemID := patchRes.Header.Get("X-Item-Id")
	if itemID == "" {
		t.Fatalf("upload %q: no X-Item-Id header", filename)
	}

	getRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+itemID, user, nil)
	if getRes.StatusCode != http.StatusOK {
		t.Fatalf("fetch uploaded item %q: got status %d", filename, getRes.StatusCode)
	}
	return decodeJSON[apiItem](t, getRes)
}

func TestUploadFlow_CreateAndUploadInOneShot(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()
	store := storage.New(ts.dataDir)

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	createFolderRes := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items", fabio, map[string]any{
		"type": "folder", "name": "Photos", "parent_id": nil,
	})
	if createFolderRes.StatusCode != http.StatusCreated {
		t.Fatalf("create Photos: got status %d", createFolderRes.StatusCode)
	}
	photos := decodeJSON[apiItem](t, createFolderRes)

	content := make([]byte, 256*1024) // 256 KiB — big enough to be a real multi-read, small enough for a fast test
	if _, err := rand.Read(content); err != nil {
		t.Fatalf("generate random content: %v", err)
	}

	metadataHeader := handler.SerializeMetadataHeader(map[string]string{
		"filename":  "sunset.jpg",
		"parent_id": photos.ID,
		"filetype":  "image/jpeg",
	})

	createUploadRes := tusRequest(t, http.MethodPost, ts.URL+"/api/v1/uploads/", fabio, nil, map[string]string{
		"Upload-Length":   strconv.Itoa(len(content)),
		"Upload-Metadata": metadataHeader,
	})
	if createUploadRes.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(createUploadRes.Body)
		t.Fatalf("create upload: got status %d, body: %s", createUploadRes.StatusCode, body)
	}
	location := createUploadRes.Header.Get("Location")
	if location == "" {
		t.Fatal("create upload: no Location header in response")
	}

	patchRes := tusRequest(t, http.MethodPatch, location, fabio, bytes.NewReader(content), map[string]string{
		"Content-Type":  "application/offset+octet-stream",
		"Upload-Offset": "0",
	})
	if patchRes.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(patchRes.Body)
		t.Fatalf("upload bytes: got status %d, body: %s", patchRes.StatusCode, body)
	}

	itemID := patchRes.Header.Get("X-Item-Id")
	if itemID == "" {
		t.Fatal("upload bytes: no X-Item-Id header — did preFinish run and finalize the upload?")
	}

	// --- verify the DB row, field by field ------------------------------------
	var (
		dbName      string
		dbType      string
		dbSizeBytes int64
		dbMimeType  string
		dbChecksum  string
		dbParentID  string
	)
	err = ts.app.DB.QueryRowContext(ctx,
		`SELECT name, type, size_bytes, mime_type, checksum, parent_id FROM items WHERE id = ?`, itemID,
	).Scan(&dbName, &dbType, &dbSizeBytes, &dbMimeType, &dbChecksum, &dbParentID)
	if err != nil {
		t.Fatalf("scan uploaded item row: %v", err)
	}
	if dbName != "sunset.jpg" || dbType != "file" || dbParentID != photos.ID {
		t.Errorf("item row = {name:%q type:%q parent_id:%q}, want {sunset.jpg file %q}", dbName, dbType, dbParentID, photos.ID)
	}
	if dbSizeBytes != int64(len(content)) {
		t.Errorf("item.size_bytes = %d, want %d", dbSizeBytes, len(content))
	}
	if dbMimeType != "image/jpeg" {
		t.Errorf("item.mime_type = %q, want %q", dbMimeType, "image/jpeg")
	}
	if dbChecksum == "" {
		t.Error("item.checksum is empty, want a sha256 hex digest")
	}

	// --- the real assertion the testing convention calls for: the file on
	// storage, compared byte-for-byte against the input ------------------------
	onDiskPath := filepath.Join(store.UserFilesRoot(fabio.username), "Photos", "sunset.jpg")
	onDiskContent, err := os.ReadFile(onDiskPath)
	if err != nil {
		t.Fatalf("read uploaded file from disk at %s: %v", onDiskPath, err)
	}
	if !bytes.Equal(onDiskContent, content) {
		t.Errorf("file on disk (%d bytes) does not match the uploaded content (%d bytes)", len(onDiskContent), len(content))
	}

	// --- quota accounting was updated to match ---------------------------------
	var storageUsed int64
	if err := ts.app.DB.QueryRowContext(ctx, `SELECT storage_used_bytes FROM users WHERE id = ?`, fabio.id).Scan(&storageUsed); err != nil {
		t.Fatalf("scan storage_used_bytes: %v", err)
	}
	if storageUsed != int64(len(content)) {
		t.Errorf("users.storage_used_bytes = %d, want %d", storageUsed, len(content))
	}

	// --- the file is a normal item now: it shows up in a listing ---------------
	listRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items?parent_id="+photos.ID, fabio, nil)
	children := decodeJSON[[]apiItem](t, listRes)
	if len(children) != 1 || children[0].ID != itemID {
		t.Fatalf("Photos listing = %+v, want just sunset.jpg", children)
	}
}

func TestUploadFlow_ResumesAcrossTwoChunks(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()
	store := storage.New(ts.dataDir)

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	content := make([]byte, 128*1024)
	if _, err := rand.Read(content); err != nil {
		t.Fatalf("generate random content: %v", err)
	}
	firstHalf, secondHalf := content[:64*1024], content[64*1024:]

	metadataHeader := handler.SerializeMetadataHeader(map[string]string{"filename": "archive.bin"})
	createRes := tusRequest(t, http.MethodPost, ts.URL+"/api/v1/uploads/", fabio, nil, map[string]string{
		"Upload-Length":   strconv.Itoa(len(content)),
		"Upload-Metadata": metadataHeader,
	})
	if createRes.StatusCode != http.StatusCreated {
		t.Fatalf("create upload: got status %d", createRes.StatusCode)
	}
	location := createRes.Header.Get("Location")

	// first chunk — simulates the connection dropping right after this
	firstRes := tusRequest(t, http.MethodPatch, location, fabio, bytes.NewReader(firstHalf), map[string]string{
		"Content-Type":  "application/offset+octet-stream",
		"Upload-Offset": "0",
	})
	if firstRes.StatusCode != http.StatusNoContent {
		t.Fatalf("upload first chunk: got status %d", firstRes.StatusCode)
	}
	if got := firstRes.Header.Get("Upload-Offset"); got != strconv.Itoa(len(firstHalf)) {
		t.Fatalf("after first chunk, Upload-Offset = %q, want %q", got, strconv.Itoa(len(firstHalf)))
	}
	if id := firstRes.Header.Get("X-Item-Id"); id != "" {
		t.Fatalf("X-Item-Id set after only a partial upload (offset %d of %d) — finalized too early", len(firstHalf), len(content))
	}

	// the client re-checks how much the server actually has before resuming —
	// this is the point of the protocol: it doesn't have to trust its own
	// memory of what it already sent
	headRes := tusRequest(t, http.MethodHead, location, fabio, nil, nil)
	if got := headRes.Header.Get("Upload-Offset"); got != strconv.Itoa(len(firstHalf)) {
		t.Fatalf("HEAD Upload-Offset = %q, want %q", got, strconv.Itoa(len(firstHalf)))
	}

	// second (resuming) chunk, picking up exactly where the first left off
	secondRes := tusRequest(t, http.MethodPatch, location, fabio, bytes.NewReader(secondHalf), map[string]string{
		"Content-Type":  "application/offset+octet-stream",
		"Upload-Offset": strconv.Itoa(len(firstHalf)),
	})
	if secondRes.StatusCode != http.StatusNoContent {
		t.Fatalf("upload second chunk: got status %d", secondRes.StatusCode)
	}
	itemID := secondRes.Header.Get("X-Item-Id")
	if itemID == "" {
		t.Fatal("upload second chunk: no X-Item-Id — upload should be complete and finalized now")
	}

	onDiskPath := filepath.Join(store.UserFilesRoot(fabio.username), "archive.bin")
	onDiskContent, err := os.ReadFile(onDiskPath)
	if err != nil {
		t.Fatalf("read uploaded file from disk at %s: %v", onDiskPath, err)
	}
	if !bytes.Equal(onDiskContent, content) {
		t.Error("file assembled from two chunks does not match the original content")
	}
}
