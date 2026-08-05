package flow

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golfarelli/denizen/internal/storage"
)

func TestCopyFlow_FileCopyIsRealIndependentBytes(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()
	store := storage.New(ts.dataDir)

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	content := []byte("the original content, byte for byte")
	original := uploadFile(t, ts, fabio, nil, "original.txt", content)

	// Copying into the same folder as the source collides with the source's
	// own name — same auto-suffix behavior as any other name collision.
	copyRes := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items/"+original.ID+"/copy", fabio, map[string]any{
		"parent_id": nil,
	})
	if copyRes.StatusCode != http.StatusCreated {
		t.Fatalf("copy: got status %d", copyRes.StatusCode)
	}
	duplicate := decodeJSON[apiItem](t, copyRes)
	if duplicate.ID == original.ID {
		t.Fatal("copy returned the same id as the original")
	}
	if duplicate.Name != "original (1).txt" {
		t.Errorf("duplicate.Name = %q, want %q", duplicate.Name, "original (1).txt")
	}

	// Real, independent bytes on disk — not a hard link, not the same file.
	originalPath := filepath.Join(store.UserFilesRoot(fabio.username), "original.txt")
	duplicatePath := filepath.Join(store.UserFilesRoot(fabio.username), "original (1).txt")
	originalBytes, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatalf("read original: %v", err)
	}
	duplicateBytes, err := os.ReadFile(duplicatePath)
	if err != nil {
		t.Fatalf("read duplicate: %v", err)
	}
	if string(duplicateBytes) != string(content) || string(originalBytes) != string(content) {
		t.Error("original and/or duplicate content does not match what was uploaded")
	}

	// Deleting the duplicate must not touch the original's bytes.
	deleteRes := authedRequest(t, http.MethodDelete, ts.URL+"/api/v1/items/"+duplicate.ID, fabio, nil)
	if deleteRes.StatusCode != http.StatusNoContent {
		t.Fatalf("delete duplicate: got status %d", deleteRes.StatusCode)
	}
	if _, err := os.Stat(originalPath); err != nil {
		t.Errorf("original.txt should still exist after deleting its copy, but: %v", err)
	}

	// Quota accounts for both copies while both existed: uploaded once,
	// copied once = 2x the file's size, even though it's since been trashed
	// (trash still counts against quota — see docs/ARCHITECTURE.md).
	var storageUsed int64
	if err := ts.app.DB.QueryRowContext(ctx, `SELECT storage_used_bytes FROM users WHERE id = ?`, fabio.id).Scan(&storageUsed); err != nil {
		t.Fatalf("scan storage_used_bytes: %v", err)
	}
	if storageUsed != int64(len(content))*2 {
		t.Errorf("storage_used_bytes = %d, want %d (original + copy, copy still in trash)", storageUsed, len(content)*2)
	}
}

func TestCopyFlow_FolderCopyIsRecursive(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()
	store := storage.New(ts.dataDir)

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	createFolderRes := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items", fabio, map[string]any{
		"type": "folder", "name": "Documents", "parent_id": nil,
	})
	if createFolderRes.StatusCode != http.StatusCreated {
		t.Fatalf("create Documents: got status %d", createFolderRes.StatusCode)
	}
	docs := decodeJSON[apiItem](t, createFolderRes)

	content := []byte("nested file content")
	nestedFile := uploadFile(t, ts, fabio, &docs.ID, "notes.txt", content)

	copyRes := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items/"+docs.ID+"/copy", fabio, map[string]any{
		"parent_id": nil,
	})
	if copyRes.StatusCode != http.StatusCreated {
		t.Fatalf("copy folder: got status %d", copyRes.StatusCode)
	}
	docsCopy := decodeJSON[apiItem](t, copyRes)
	if docsCopy.Name != "Documents (1)" {
		t.Errorf("docsCopy.Name = %q, want %q", docsCopy.Name, "Documents (1)")
	}

	listRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items?parent_id="+docsCopy.ID, fabio, nil)
	children := decodeJSON[[]apiItem](t, listRes)
	if len(children) != 1 || children[0].Name != "notes.txt" || children[0].ID == nestedFile.ID {
		t.Fatalf("Documents (1) children = %+v, want a distinct copy of notes.txt", children)
	}

	nestedCopyPath := filepath.Join(store.UserFilesRoot(fabio.username), "Documents (1)", "notes.txt")
	nestedCopyBytes, err := os.ReadFile(nestedCopyPath)
	if err != nil {
		t.Fatalf("read nested copy: %v", err)
	}
	if string(nestedCopyBytes) != string(content) {
		t.Error("nested copy content does not match the original nested file")
	}
}

func TestCopyFlow_RejectedOverQuota(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	bootstrapCode, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", bootstrapCode, created, err)
	}
	fabio := registerAndLogin(t, ts, bootstrapCode, "fabio", "correct-horse-battery-staple")

	smallQuota := int64(1000)
	guestCode, _, err := ts.app.Auth.CreateInvite(ctx, fabio.id, &smallQuota, time.Hour)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	guest := registerAndLogin(t, ts, guestCode, "guest", "another-strong-password")

	// 600 bytes: fits once (600/1000) but not twice (1200 > 1000).
	content := make([]byte, 600)
	uploaded := uploadFile(t, ts, guest, nil, "big.bin", content)

	copyRes := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items/"+uploaded.ID+"/copy", guest, map[string]any{
		"parent_id": nil,
	})
	if copyRes.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("copy over quota: got status %d, want %d", copyRes.StatusCode, http.StatusRequestEntityTooLarge)
	}

	listRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items", guest, nil)
	items := decodeJSON[[]apiItem](t, listRes)
	if len(items) != 1 {
		t.Errorf("guest's items = %+v, want just the original upload — the rejected copy must not exist", items)
	}
}
