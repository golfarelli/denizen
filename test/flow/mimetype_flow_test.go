package flow

import (
	"net/http"
	"testing"
	"time"
)

// TestMimeTypeFlow_SetOnFileWithNone reproduces the exact bug this endpoint
// exists to fix (see ItemRepository.UpdateMimeType's own comment): a file
// uploaded with no extension in its name gets no mime type at all (Go's
// mime.TypeByExtension has nothing to guess from), which makes the
// frontend's previewKind() fall through to "unsupported". SetMimeType is
// the only way to correct that in place without losing the item's id (and
// with it, every link already pointing at it).
func TestMimeTypeFlow_SetOnFileWithNone(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	alice := registerAndLogin(t, ts, code, "alice", "correct-horse-battery-staple")

	item := uploadFile(t, ts, alice, nil, "Ricevuta pagamento", []byte("%PDF-1.4 fake content"))
	if item.MimeType != nil {
		t.Fatalf("uploaded file with no extension in name already has mime_type %q, test assumption broken", *item.MimeType)
	}

	setRes := authedRequest(t, http.MethodPatch, ts.URL+"/api/v1/items/"+item.ID+"/mimetype", alice, map[string]any{
		"mime_type": "application/pdf",
	})
	if setRes.StatusCode != http.StatusOK {
		t.Fatalf("SetMimeType: got status %d", setRes.StatusCode)
	}
	updated := decodeJSON[apiItem](t, setRes)
	if updated.MimeType == nil || *updated.MimeType != "application/pdf" {
		t.Errorf("response MimeType = %v, want application/pdf", updated.MimeType)
	}
	// Name and parent must be untouched — this endpoint only ever fixes one field.
	if updated.Name != "Ricevuta pagamento" || updated.ParentID != nil {
		t.Errorf("SetMimeType changed name/parent: name=%q parent_id=%v", updated.Name, updated.ParentID)
	}

	getRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+item.ID, alice, nil)
	fetched := decodeJSON[apiItem](t, getRes)
	if fetched.MimeType == nil || *fetched.MimeType != "application/pdf" {
		t.Errorf("persisted MimeType = %v, want application/pdf (fix didn't survive a fresh GET)", fetched.MimeType)
	}
}

func TestMimeTypeFlow_RejectsEmptyAndFolders(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	alice := registerAndLogin(t, ts, code, "alice", "correct-horse-battery-staple")

	item := uploadFile(t, ts, alice, nil, "file.txt", []byte("hello"))

	emptyRes := authedRequest(t, http.MethodPatch, ts.URL+"/api/v1/items/"+item.ID+"/mimetype", alice, map[string]any{
		"mime_type": "",
	})
	if emptyRes.StatusCode != http.StatusBadRequest {
		t.Errorf("empty mime_type: got status %d, want 400", emptyRes.StatusCode)
	}

	folderRes := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items", alice, map[string]any{
		"type": "folder", "name": "A folder", "parent_id": nil,
	})
	folder := decodeJSON[apiItem](t, folderRes)
	onFolderRes := authedRequest(t, http.MethodPatch, ts.URL+"/api/v1/items/"+folder.ID+"/mimetype", alice, map[string]any{
		"mime_type": "application/pdf",
	})
	if onFolderRes.StatusCode != http.StatusBadRequest {
		t.Errorf("mimetype on a folder: got status %d, want 400", onFolderRes.StatusCode)
	}
}
