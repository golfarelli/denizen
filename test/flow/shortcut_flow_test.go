package flow

import (
	"io"
	"net/http"
	"testing"
	"time"
)

func createShortcut(t *testing.T, ts *testServer, caller registeredUser, targetID string, parentID *string) *http.Response {
	t.Helper()
	return authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items/"+targetID+"/shortcut", caller, map[string]any{
		"parent_id": parentID,
	})
}

// TestShortcutFlow_PureAlias covers the core "alias puro" contract in one
// pass: a shortcut shows up with target_id and no storage of its own,
// streams the real target's bytes, and renaming/moving/deleting it never
// touches the original.
func TestShortcutFlow_PureAlias(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	alice := registerAndLogin(t, ts, code, "alice", "correct-horse-battery-staple")

	content := []byte("the real file's real content")
	original := uploadFile(t, ts, alice, nil, "original.txt", content)

	createFolderRes := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items", alice, map[string]any{
		"type": "folder", "name": "Elsewhere", "parent_id": nil,
	})
	if createFolderRes.StatusCode != http.StatusCreated {
		t.Fatalf("create Elsewhere: got status %d", createFolderRes.StatusCode)
	}
	elsewhere := decodeJSON[apiItem](t, createFolderRes)

	// --- create --------------------------------------------------------------------
	shortcutRes := createShortcut(t, ts, alice, original.ID, &elsewhere.ID)
	if shortcutRes.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(shortcutRes.Body)
		t.Fatalf("create shortcut: got status %d, body: %s", shortcutRes.StatusCode, body)
	}
	shortcut := decodeJSON[apiItem](t, shortcutRes)
	if shortcut.ID == original.ID {
		t.Fatal("shortcut got the same id as the original")
	}
	if shortcut.TargetID == nil || *shortcut.TargetID != original.ID {
		t.Fatalf("shortcut.TargetID = %v, want %q", shortcut.TargetID, original.ID)
	}
	if shortcut.Name != original.Name {
		t.Errorf("shortcut.Name = %q, want a snapshot of the original's %q", shortcut.Name, original.Name)
	}
	if shortcut.SizeBytes != 0 {
		t.Errorf("shortcut.SizeBytes = %d, want 0 — a shortcut has no storage of its own", shortcut.SizeBytes)
	}

	// No quota consumed by the shortcut itself.
	var storageUsed int64
	if err := ts.app.DB.QueryRowContext(ctx, `SELECT storage_used_bytes FROM users WHERE id = ?`, alice.id).Scan(&storageUsed); err != nil {
		t.Fatalf("scan storage_used_bytes: %v", err)
	}
	if storageUsed != int64(len(content)) {
		t.Errorf("storage_used_bytes = %d, want %d (original only, shortcut is free)", storageUsed, len(content))
	}

	// --- download streams the real target's bytes -----------------------------------
	contentRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+shortcut.ID+"/content", alice, nil)
	if contentRes.StatusCode != http.StatusOK {
		t.Fatalf("download via shortcut: got status %d", contentRes.StatusCode)
	}
	got, err := io.ReadAll(contentRes.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Errorf("content via shortcut = %q, want %q", got, content)
	}

	// --- rename touches only the shortcut's own row ----------------------------------
	renameRes := authedRequest(t, http.MethodPatch, ts.URL+"/api/v1/items/"+shortcut.ID, alice, map[string]any{
		"name": "renamed-locally.txt", "parent_id": elsewhere.ID,
	})
	if renameRes.StatusCode != http.StatusOK {
		t.Fatalf("rename shortcut: got status %d", renameRes.StatusCode)
	}
	getOriginalRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+original.ID, alice, nil)
	stillOriginal := decodeJSON[apiItem](t, getOriginalRes)
	if stillOriginal.Name != "original.txt" {
		t.Errorf("original.Name = %q after renaming the shortcut, want untouched %q", stillOriginal.Name, "original.txt")
	}

	// --- move touches only the shortcut's own row ------------------------------------
	moveRes := authedRequest(t, http.MethodPatch, ts.URL+"/api/v1/items/"+shortcut.ID, alice, map[string]any{
		"name": "renamed-locally.txt", "parent_id": nil,
	})
	if moveRes.StatusCode != http.StatusOK {
		t.Fatalf("move shortcut: got status %d", moveRes.StatusCode)
	}
	moved := decodeJSON[apiItem](t, moveRes)
	if moved.ParentID != nil {
		t.Errorf("moved shortcut ParentID = %v, want nil (root)", moved.ParentID)
	}
	getOriginalRes2 := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+original.ID, alice, nil)
	stillOriginal2 := decodeJSON[apiItem](t, getOriginalRes2)
	if stillOriginal2.ParentID != nil {
		t.Errorf("original.ParentID = %v after moving the shortcut, want untouched nil", stillOriginal2.ParentID)
	}

	// --- copy creates another shortcut, not a content duplicate ----------------------
	copyRes := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items/"+shortcut.ID+"/copy", alice, map[string]any{
		"parent_id": elsewhere.ID,
	})
	if copyRes.StatusCode != http.StatusCreated {
		t.Fatalf("copy shortcut: got status %d", copyRes.StatusCode)
	}
	shortcutCopy := decodeJSON[apiItem](t, copyRes)
	if shortcutCopy.TargetID == nil || *shortcutCopy.TargetID != original.ID {
		t.Errorf("copied shortcut.TargetID = %v, want %q (still pointing at the same real file)", shortcutCopy.TargetID, original.ID)
	}
	if shortcutCopy.SizeBytes != 0 {
		t.Errorf("copied shortcut.SizeBytes = %d, want 0", shortcutCopy.SizeBytes)
	}

	// --- delete removes only the pointer, never the original -------------------------
	deleteRes := authedRequest(t, http.MethodDelete, ts.URL+"/api/v1/items/"+shortcut.ID, alice, nil)
	if deleteRes.StatusCode != http.StatusNoContent {
		t.Fatalf("delete shortcut: got status %d", deleteRes.StatusCode)
	}
	originalStillThereRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+original.ID+"/content", alice, nil)
	if originalStillThereRes.StatusCode != http.StatusOK {
		t.Fatalf("original content after deleting its shortcut: got status %d, want 200", originalStillThereRes.StatusCode)
	}
	stillGot, _ := io.ReadAll(originalStillThereRes.Body)
	if string(stillGot) != string(content) {
		t.Error("original content changed after deleting its shortcut")
	}
}

// TestShortcutFlow_ToSharedItem covers the two-user case the wishlist note
// explicitly asked for: a shortcut from "Shared with me" into the
// recipient's own drive — and that revoking the underlying share actually
// breaks it, rather than the shortcut silently keeping access alive off
// whatever was true at creation time.
func TestShortcutFlow_ToSharedItem(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	alice := registerAndLogin(t, ts, code, "alice", "correct-horse-battery-staple")

	annaCode, _, err := ts.app.Auth.CreateInvite(ctx, alice.id, nil, time.Hour)
	if err != nil {
		t.Fatalf("CreateInvite (anna): %v", err)
	}
	anna := registerAndLogin(t, ts, annaCode, "anna", "another-strong-password")

	content := []byte("something alice shared with anna")
	shared := uploadFile(t, ts, alice, nil, "shared.txt", content)

	grantRes := createUserShare(t, ts, alice, shared.ID, anna.id)
	if grantRes.StatusCode != http.StatusCreated {
		t.Fatalf("create user-share: got status %d", grantRes.StatusCode)
	}
	grant := decodeJSON[userShareResponse](t, grantRes)

	// Anna adds a shortcut to it in her own drive.
	shortcutRes := createShortcut(t, ts, anna, shared.ID, nil)
	if shortcutRes.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(shortcutRes.Body)
		t.Fatalf("anna create shortcut: got status %d, body: %s", shortcutRes.StatusCode, body)
	}
	shortcut := decodeJSON[apiItem](t, shortcutRes)

	// Works while the share is active.
	contentRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+shortcut.ID+"/content", anna, nil)
	if contentRes.StatusCode != http.StatusOK {
		t.Fatalf("anna download via shortcut before revoke: got status %d", contentRes.StatusCode)
	}
	got, _ := io.ReadAll(contentRes.Body)
	if string(got) != string(content) {
		t.Errorf("content via shortcut = %q, want %q", got, content)
	}

	// Alice revokes the share.
	revokeRes := authedRequest(t, http.MethodDelete, ts.URL+"/api/v1/user-shares/"+grant.ID, alice, nil)
	if revokeRes.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke share: got status %d", revokeRes.StatusCode)
	}

	// The shortcut row still exists in anna's own drive (it's hers to
	// delete or keep) but is now broken — re-verified against the target
	// at read time, not trusted from creation time (ResolveContentItem).
	brokenRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+shortcut.ID+"/content", anna, nil)
	if brokenRes.StatusCode != http.StatusNotFound {
		t.Fatalf("anna download via shortcut after revoke: got status %d, want 404 (access re-checked, not cached from creation)", brokenRes.StatusCode)
	}
	listRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items", anna, nil)
	items := decodeJSON[[]apiItem](t, listRes)
	found := false
	for _, it := range items {
		if it.ID == shortcut.ID {
			found = true
		}
	}
	if !found {
		t.Error("anna's shortcut row disappeared from her own listing after the share was revoked — should still be there, just broken")
	}
}
