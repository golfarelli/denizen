package flow

import (
	"io"
	"net/http"
	"testing"
	"time"
)

type userShareResponse struct {
	ID                 string `json:"id"`
	ItemID             string `json:"item_id"`
	SharedWithUsername string `json:"shared_with_username,omitempty"`
	OwnerUsername      string `json:"owner_username,omitempty"`
	CreatedAt          int64  `json:"created_at"`
}

type directoryUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

func createUserShare(t *testing.T, ts *testServer, owner registeredUser, itemID, targetUserID string) *http.Response {
	t.Helper()
	return authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items/"+itemID+"/user-shares", owner, map[string]any{
		"user_id": targetUserID,
	})
}

func TestUserShareFlow_GrantsViewOnlyAccessAndCanBeRevoked(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	marioCode, _, err := ts.app.Auth.CreateInvite(ctx, fabio.id, nil, time.Hour)
	if err != nil {
		t.Fatalf("CreateInvite (mario): %v", err)
	}
	mario := registerAndLogin(t, ts, marioCode, "mario", "another-strong-password")

	luigiCode, _, err := ts.app.Auth.CreateInvite(ctx, fabio.id, nil, time.Hour)
	if err != nil {
		t.Fatalf("CreateInvite (luigi): %v", err)
	}
	luigi := registerAndLogin(t, ts, luigiCode, "luigi", "yet-another-password")

	content := []byte("a document fabio wants to share directly with mario, not luigi")
	item := uploadFile(t, ts, fabio, nil, "recipe.txt", content)

	// --- before any grant, neither mario nor luigi can see it --------------------
	for _, u := range []registeredUser{mario, luigi} {
		res := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+item.ID, u, nil)
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("%s GET item before any grant: got status %d, want 404", u.username, res.StatusCode)
		}
	}

	// --- fabio shares it with mario specifically ---------------------------------
	createRes := createUserShare(t, ts, fabio, item.ID, mario.id)
	if createRes.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(createRes.Body)
		t.Fatalf("create user-share: got status %d, body: %s", createRes.StatusCode, body)
	}
	grant := decodeJSON[userShareResponse](t, createRes)
	if grant.SharedWithUsername != "mario" || grant.ItemID != item.ID {
		t.Errorf("create user-share response = %+v, want shared_with_username=mario item_id=%s", grant, item.ID)
	}

	// --- mario can now view and download it ---------------------------------------
	getRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+item.ID, mario, nil)
	if getRes.StatusCode != http.StatusOK {
		t.Fatalf("mario GET shared item: got status %d, want 200", getRes.StatusCode)
	}
	gotItem := decodeJSON[apiItem](t, getRes)
	if gotItem.Name != "recipe.txt" {
		t.Errorf("mario's view of the shared item = %+v, want name=recipe.txt", gotItem)
	}

	contentRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+item.ID+"/content", mario, nil)
	if contentRes.StatusCode != http.StatusOK {
		t.Fatalf("mario GET shared item content: got status %d, want 200", contentRes.StatusCode)
	}
	gotContent, _ := io.ReadAll(contentRes.Body)
	if string(gotContent) != string(content) {
		t.Errorf("mario downloaded %q, want %q", gotContent, content)
	}

	// --- luigi (not granted) still can't see it -----------------------------------
	luigiRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+item.ID, luigi, nil)
	if luigiRes.StatusCode != http.StatusNotFound {
		t.Fatalf("luigi GET item mario was shared: got status %d, want 404", luigiRes.StatusCode)
	}

	// --- view-only: mario cannot rename, move, delete, or re-share it ------------
	renameRes := authedRequest(t, http.MethodPatch, ts.URL+"/api/v1/items/"+item.ID, mario, map[string]any{
		"name": "hijacked.txt", "parent_id": nil,
	})
	if renameRes.StatusCode != http.StatusNotFound {
		t.Errorf("mario PATCH (rename) shared item: got status %d, want 404 (mutating ops must stay owner-only)", renameRes.StatusCode)
	}
	deleteRes := authedRequest(t, http.MethodDelete, ts.URL+"/api/v1/items/"+item.ID, mario, nil)
	if deleteRes.StatusCode != http.StatusNotFound {
		t.Errorf("mario DELETE shared item: got status %d, want 404", deleteRes.StatusCode)
	}
	reshareRes := createUserShare(t, ts, mario, item.ID, luigi.id)
	if reshareRes.StatusCode != http.StatusNotFound {
		t.Errorf("mario re-sharing an item he doesn't own: got status %d, want 404", reshareRes.StatusCode)
	}

	// --- fabio's own "who has access" listing shows mario -----------------------
	listForItemRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+item.ID+"/user-shares", fabio, nil)
	if listForItemRes.StatusCode != http.StatusOK {
		t.Fatalf("list user-shares for item: got status %d", listForItemRes.StatusCode)
	}
	forItem := decodeJSON[[]userShareResponse](t, listForItemRes)
	if len(forItem) != 1 || forItem[0].SharedWithUsername != "mario" {
		t.Errorf("list user-shares for item = %+v, want exactly one grant to mario", forItem)
	}

	// --- mario's own "Shared with me" listing shows fabio's file ----------------
	receivedRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/shared-with-me", mario, nil)
	if receivedRes.StatusCode != http.StatusOK {
		t.Fatalf("GET shared-with-me: got status %d", receivedRes.StatusCode)
	}
	received := decodeJSON[[]userShareResponse](t, receivedRes)
	if len(received) != 1 || received[0].ItemID != item.ID || received[0].OwnerUsername != "fabio" {
		t.Errorf("mario's shared-with-me = %+v, want exactly one grant from fabio for item %s", received, item.ID)
	}
	// luigi has nothing shared with him
	luigiReceivedRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/shared-with-me", luigi, nil)
	luigiReceived := decodeJSON[[]userShareResponse](t, luigiReceivedRes)
	if len(luigiReceived) != 0 {
		t.Errorf("luigi's shared-with-me = %+v, want empty", luigiReceived)
	}

	// --- only the owner can revoke, not the recipient -----------------------------
	marioRevokeRes := authedRequest(t, http.MethodDelete, ts.URL+"/api/v1/user-shares/"+grant.ID, mario, nil)
	if marioRevokeRes.StatusCode != http.StatusNotFound {
		t.Errorf("mario revoking his own received grant: got status %d, want 404 (only the owner may revoke)", marioRevokeRes.StatusCode)
	}

	// --- fabio revokes it — mario loses access immediately ------------------------
	revokeRes := authedRequest(t, http.MethodDelete, ts.URL+"/api/v1/user-shares/"+grant.ID, fabio, nil)
	if revokeRes.StatusCode != http.StatusNoContent {
		t.Fatalf("fabio revoke: got status %d, want 204", revokeRes.StatusCode)
	}
	afterRevokeRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+item.ID, mario, nil)
	if afterRevokeRes.StatusCode != http.StatusNotFound {
		t.Fatalf("mario GET item after revoke: got status %d, want 404", afterRevokeRes.StatusCode)
	}
}

func TestUserShareFlow_ValidationRejectsFoldersSelfDuplicatesAndUnknownUsers(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	marioCode, _, err := ts.app.Auth.CreateInvite(ctx, fabio.id, nil, time.Hour)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	mario := registerAndLogin(t, ts, marioCode, "mario", "another-strong-password")

	// --- folders can't be shared directly yet -------------------------------------
	folderRes := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items", fabio, map[string]any{
		"type": "folder", "name": "Vacanze", "parent_id": nil,
	})
	folder := decodeJSON[apiItem](t, folderRes)
	folderShareRes := createUserShare(t, ts, fabio, folder.ID, mario.id)
	if folderShareRes.StatusCode != http.StatusBadRequest {
		t.Errorf("sharing a folder: got status %d, want 400", folderShareRes.StatusCode)
	}

	item := uploadFile(t, ts, fabio, nil, "doc.txt", []byte("hello"))

	// --- can't share with yourself -------------------------------------------------
	selfShareRes := createUserShare(t, ts, fabio, item.ID, fabio.id)
	if selfShareRes.StatusCode != http.StatusBadRequest {
		t.Errorf("sharing with self: got status %d, want 400", selfShareRes.StatusCode)
	}

	// --- can't share with a nonexistent user ----------------------------------------
	unknownRes := createUserShare(t, ts, fabio, item.ID, "not-a-real-user-id")
	if unknownRes.StatusCode != http.StatusBadRequest {
		t.Errorf("sharing with an unknown user id: got status %d, want 400", unknownRes.StatusCode)
	}

	// --- a real grant, then a duplicate is rejected ----------------------------------
	firstRes := createUserShare(t, ts, fabio, item.ID, mario.id)
	if firstRes.StatusCode != http.StatusCreated {
		t.Fatalf("first share: got status %d", firstRes.StatusCode)
	}
	dupRes := createUserShare(t, ts, fabio, item.ID, mario.id)
	if dupRes.StatusCode != http.StatusConflict {
		t.Errorf("duplicate share with the same person: got status %d, want 409", dupRes.StatusCode)
	}
}

func TestUserShareFlow_TrashedItemIsInaccessibleToTheRecipient(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	marioCode, _, err := ts.app.Auth.CreateInvite(ctx, fabio.id, nil, time.Hour)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	mario := registerAndLogin(t, ts, marioCode, "mario", "another-strong-password")

	item := uploadFile(t, ts, fabio, nil, "doc.txt", []byte("hello"))
	createRes := createUserShare(t, ts, fabio, item.ID, mario.id)
	if createRes.StatusCode != http.StatusCreated {
		t.Fatalf("create user-share: got status %d", createRes.StatusCode)
	}

	// Sanity: mario can see it before it's trashed.
	beforeRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+item.ID, mario, nil)
	if beforeRes.StatusCode != http.StatusOK {
		t.Fatalf("mario GET before trash: got status %d, want 200", beforeRes.StatusCode)
	}

	deleteRes := authedRequest(t, http.MethodDelete, ts.URL+"/api/v1/items/"+item.ID, fabio, nil)
	if deleteRes.StatusCode != http.StatusNoContent {
		t.Fatalf("fabio delete (trash) the item: got status %d", deleteRes.StatusCode)
	}

	// A grant never reaches into the owner's trash — unlike the owner's own
	// GetIncludingTrashed path, this should read as "not found", not as a
	// trash-preview window the recipient has no restore button for anyway.
	afterRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+item.ID, mario, nil)
	if afterRes.StatusCode != http.StatusNotFound {
		t.Fatalf("mario GET after fabio trashed it: got status %d, want 404", afterRes.StatusCode)
	}
}

func TestUserShareFlow_DirectoryExcludesSelfAndDisabledUsers(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	marioCode, _, err := ts.app.Auth.CreateInvite(ctx, fabio.id, nil, time.Hour)
	if err != nil {
		t.Fatalf("CreateInvite (mario): %v", err)
	}
	// Registered (so the directory has more than one other real user to
	// filter down to) but never referenced beyond that — the assertion
	// below checks the resulting directory list's content directly.
	registerAndLogin(t, ts, marioCode, "mario", "another-strong-password")

	luigiCode, _, err := ts.app.Auth.CreateInvite(ctx, fabio.id, nil, time.Hour)
	if err != nil {
		t.Fatalf("CreateInvite (luigi): %v", err)
	}
	luigi := registerAndLogin(t, ts, luigiCode, "luigi", "yet-another-password")

	// Disable luigi via the admin endpoint.
	disableRes := authedRequest(t, http.MethodPatch, ts.URL+"/api/v1/users/"+luigi.id, fabio, map[string]any{
		"disabled": true,
	})
	if disableRes.StatusCode != http.StatusOK {
		t.Fatalf("disable luigi: got status %d", disableRes.StatusCode)
	}

	dirRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/users/directory", fabio, nil)
	if dirRes.StatusCode != http.StatusOK {
		t.Fatalf("GET directory: got status %d", dirRes.StatusCode)
	}
	directory := decodeJSON[[]directoryUser](t, dirRes)
	if len(directory) != 1 || directory[0].Username != "mario" {
		t.Errorf("directory (as fabio) = %+v, want exactly [mario] (self and disabled luigi excluded)", directory)
	}
}
