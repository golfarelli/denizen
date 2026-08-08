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
	Permission         string `json:"permission"`
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

func createUserShareWithPermission(t *testing.T, ts *testServer, owner registeredUser, itemID, targetUserID, permission string) *http.Response {
	t.Helper()
	return authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items/"+itemID+"/user-shares", owner, map[string]any{
		"user_id": targetUserID, "permission": permission,
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

	// --- view-only: mario can see it (200s above) but can't write to it,
	// which now reads as 403 (he has *some* access, just not enough),
	// unlike luigi's 404 above (no access to the item at all) --------------
	renameRes := authedRequest(t, http.MethodPatch, ts.URL+"/api/v1/items/"+item.ID, mario, map[string]any{
		"name": "hijacked.txt", "parent_id": nil,
	})
	if renameRes.StatusCode != http.StatusForbidden {
		t.Errorf("mario PATCH (rename) view-only shared item: got status %d, want 403", renameRes.StatusCode)
	}
	deleteRes := authedRequest(t, http.MethodDelete, ts.URL+"/api/v1/items/"+item.ID, mario, nil)
	if deleteRes.StatusCode != http.StatusForbidden {
		t.Errorf("mario DELETE view-only shared item: got status %d, want 403", deleteRes.StatusCode)
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

	// --- folders can be shared too, same as files -----------------------------------
	folderRes := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items", fabio, map[string]any{
		"type": "folder", "name": "Vacanze", "parent_id": nil,
	})
	folder := decodeJSON[apiItem](t, folderRes)
	folderShareRes := createUserShare(t, ts, fabio, folder.ID, mario.id)
	if folderShareRes.StatusCode != http.StatusCreated {
		t.Errorf("sharing a folder: got status %d, want 201", folderShareRes.StatusCode)
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

// TestUserShareFlow_EditPermissionAllowsWriteAndCanBeChangedLater covers the
// view/edit permission level itself: a view grant still can't write
// (already covered by the 403s in the first test above), an edit grant
// can, and PATCH /user-shares/{id} can flip an existing grant between the
// two without revoking and re-sharing.
func TestUserShareFlow_EditPermissionAllowsWriteAndCanBeChangedLater(t *testing.T) {
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

	grantRes := createUserShareWithPermission(t, ts, fabio, item.ID, mario.id, "edit")
	if grantRes.StatusCode != http.StatusCreated {
		t.Fatalf("share at edit: got status %d", grantRes.StatusCode)
	}
	grant := decodeJSON[userShareResponse](t, grantRes)
	if grant.Permission != "edit" {
		t.Errorf("grant.Permission = %q, want edit", grant.Permission)
	}

	// --- mario, with edit access, can rename and move it -------------------------
	renameRes := authedRequest(t, http.MethodPatch, ts.URL+"/api/v1/items/"+item.ID, mario, map[string]any{
		"name": "renamed-by-mario.txt", "parent_id": nil,
	})
	if renameRes.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(renameRes.Body)
		t.Fatalf("mario PATCH (rename) edit-shared item: got status %d, body: %s", renameRes.StatusCode, body)
	}
	renamed := decodeJSON[apiItem](t, renameRes)
	if renamed.Name != "renamed-by-mario.txt" {
		t.Errorf("renamed item name = %q, want renamed-by-mario.txt", renamed.Name)
	}
	// Ownership never transfers — mario edited it, fabio still owns it.
	var ownerAfterRename string
	if err := ts.app.DB.QueryRowContext(ctx, `SELECT owner_id FROM items WHERE id = ?`, item.ID).Scan(&ownerAfterRename); err != nil {
		t.Fatalf("scan owner_id: %v", err)
	}
	if ownerAfterRename != fabio.id {
		t.Errorf("owner_id after mario's rename = %q, want fabio's id %q", ownerAfterRename, fabio.id)
	}

	// --- fabio downgrades the grant to view-only ----------------------------------
	downgradeRes := authedRequest(t, http.MethodPatch, ts.URL+"/api/v1/user-shares/"+grant.ID, fabio, map[string]any{
		"permission": "view",
	})
	if downgradeRes.StatusCode != http.StatusOK {
		t.Fatalf("downgrade grant to view: got status %d", downgradeRes.StatusCode)
	}
	downgraded := decodeJSON[userShareResponse](t, downgradeRes)
	if downgraded.Permission != "view" {
		t.Errorf("downgraded grant.Permission = %q, want view", downgraded.Permission)
	}

	// --- mario can no longer write it, but can still read it ---------------------
	deleteRes := authedRequest(t, http.MethodDelete, ts.URL+"/api/v1/items/"+item.ID, mario, nil)
	if deleteRes.StatusCode != http.StatusForbidden {
		t.Errorf("mario DELETE after downgrade to view: got status %d, want 403", deleteRes.StatusCode)
	}
	getRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+item.ID, mario, nil)
	if getRes.StatusCode != http.StatusOK {
		t.Errorf("mario GET after downgrade to view: got status %d, want 200 (still readable)", getRes.StatusCode)
	}

	// --- mario (the recipient, not the owner) may not change his own grant -------
	selfUpgradeRes := authedRequest(t, http.MethodPatch, ts.URL+"/api/v1/user-shares/"+grant.ID, mario, map[string]any{
		"permission": "edit",
	})
	if selfUpgradeRes.StatusCode != http.StatusNotFound {
		t.Errorf("mario upgrading his own received grant: got status %d, want 404 (only the owner may change it)", selfUpgradeRes.StatusCode)
	}
}

// TestUserShareFlow_FolderShareIsInheritedByEverythingInsideIt covers the
// other half of "like Google Drive": sharing a folder gives access to its
// whole subtree, at edit permission real collaboration (upload, create
// subfolder), and whatever a grant recipient creates still lands in the
// real owner's drive and quota, not their own.
func TestUserShareFlow_FolderShareIsInheritedByEverythingInsideIt(t *testing.T) {
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

	folderRes := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items", fabio, map[string]any{
		"type": "folder", "name": "Team", "parent_id": nil,
	})
	folder := decodeJSON[apiItem](t, folderRes)
	existing := uploadFile(t, ts, fabio, &folder.ID, "already-here.txt", []byte("preexisting"))

	if res := createUserShareWithPermission(t, ts, fabio, folder.ID, mario.id, "view"); res.StatusCode != http.StatusCreated {
		t.Fatalf("share folder at view: got status %d", res.StatusCode)
	}

	// --- view: mario can browse in and see the file already inside it, but
	// can neither upload nor create a subfolder --------------------------------
	listRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items?parent_id="+folder.ID, mario, nil)
	if listRes.StatusCode != http.StatusOK {
		t.Fatalf("mario list view-shared folder: got status %d, want 200", listRes.StatusCode)
	}
	children := decodeJSON[[]apiItem](t, listRes)
	if len(children) != 1 || children[0].ID != existing.ID || children[0].CanEdit {
		t.Errorf("mario's listing of the view-shared folder = %+v, want exactly [%s] with can_edit=false", children, existing.ID)
	}

	subfolderRes := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items", mario, map[string]any{
		"type": "folder", "name": "Sub", "parent_id": folder.ID,
	})
	if subfolderRes.StatusCode != http.StatusForbidden {
		t.Errorf("mario creating a subfolder in a view-shared folder: got status %d, want 403", subfolderRes.StatusCode)
	}

	// --- fabio upgrades the share to edit ------------------------------------------
	grants := decodeJSON[[]userShareResponse](t, mustGet(t, ts, fabio, "/api/v1/items/"+folder.ID+"/user-shares"))
	if len(grants) != 1 {
		t.Fatalf("grants for folder = %+v, want exactly one", grants)
	}
	if res := authedRequest(t, http.MethodPatch, ts.URL+"/api/v1/user-shares/"+grants[0].ID, fabio, map[string]any{
		"permission": "edit",
	}); res.StatusCode != http.StatusOK {
		t.Fatalf("upgrade folder grant to edit: got status %d", res.StatusCode)
	}

	// --- edit: mario can now create a subfolder and upload into it, both
	// landing in fabio's drive (owner_id) and counting against fabio's quota,
	// not mario's --------------------------------------------------------------
	subfolderRes2 := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items", mario, map[string]any{
		"type": "folder", "name": "Sub", "parent_id": folder.ID,
	})
	if subfolderRes2.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(subfolderRes2.Body)
		t.Fatalf("mario creating a subfolder in an edit-shared folder: got status %d, body: %s", subfolderRes2.StatusCode, body)
	}
	subfolder := decodeJSON[apiItem](t, subfolderRes2)

	content := []byte("uploaded by mario, owned by fabio")
	uploaded := uploadFile(t, ts, mario, &subfolder.ID, "from-mario.txt", content)

	var uploadedOwnerID string
	if err := ts.app.DB.QueryRowContext(ctx, `SELECT owner_id FROM items WHERE id = ?`, uploaded.ID).Scan(&uploadedOwnerID); err != nil {
		t.Fatalf("scan owner_id of mario's upload: %v", err)
	}
	if uploadedOwnerID != fabio.id {
		t.Errorf("owner_id of the file mario uploaded into fabio's shared folder = %q, want fabio's id %q", uploadedOwnerID, fabio.id)
	}

	var fabioStorageUsed, marioStorageUsed int64
	if err := ts.app.DB.QueryRowContext(ctx, `SELECT storage_used_bytes FROM users WHERE id = ?`, fabio.id).Scan(&fabioStorageUsed); err != nil {
		t.Fatalf("scan fabio's storage_used_bytes: %v", err)
	}
	if err := ts.app.DB.QueryRowContext(ctx, `SELECT storage_used_bytes FROM users WHERE id = ?`, mario.id).Scan(&marioStorageUsed); err != nil {
		t.Fatalf("scan mario's storage_used_bytes: %v", err)
	}
	wantFabioStorageUsed := int64(len("preexisting") + len(content))
	if fabioStorageUsed != wantFabioStorageUsed {
		t.Errorf("fabio's storage_used_bytes = %d, want %d (his own upload + what mario uploaded into his shared folder)", fabioStorageUsed, wantFabioStorageUsed)
	}
	if marioStorageUsed != 0 {
		t.Errorf("mario's storage_used_bytes = %d, want 0 (nothing he uploads into someone else's shared folder counts against him)", marioStorageUsed)
	}

	// --- can't move a shared item across into a different owner's drive ----------
	marioOwnFolderRes := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items", mario, map[string]any{
		"type": "folder", "name": "MarioOwn", "parent_id": nil,
	})
	marioOwnFolder := decodeJSON[apiItem](t, marioOwnFolderRes)
	crossOwnerMoveRes := authedRequest(t, http.MethodPatch, ts.URL+"/api/v1/items/"+uploaded.ID, mario, map[string]any{
		"name": uploaded.Name, "parent_id": marioOwnFolder.ID,
	})
	if crossOwnerMoveRes.StatusCode != http.StatusBadRequest {
		t.Errorf("mario moving a shared item into his own drive: got status %d, want 400", crossOwnerMoveRes.StatusCode)
	}
}

// TestUserShareFlow_OwnerSeesWhoAnItemIsSharedWith covers the "who has
// access" badge: List/Get expose shared_with on an owned item, and never
// on one the caller only has a grant to (that's the owner's own
// information to see, not the recipient's).
func TestUserShareFlow_OwnerSeesWhoAnItemIsSharedWith(t *testing.T) {
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
	if res := createUserShare(t, ts, fabio, item.ID, mario.id); res.StatusCode != http.StatusCreated {
		t.Fatalf("share: got status %d", res.StatusCode)
	}

	fabioGet := decodeJSON[apiItem](t, mustGet(t, ts, fabio, "/api/v1/items/"+item.ID))
	if len(fabioGet.SharedWith) != 1 || fabioGet.SharedWith[0] != "mario" {
		t.Errorf("fabio's own GET of his shared item: shared_with = %+v, want [mario]", fabioGet.SharedWith)
	}

	fabioList := decodeJSON[[]apiItem](t, mustGet(t, ts, fabio, "/api/v1/items"))
	var listedRow *apiItem
	for i := range fabioList {
		if fabioList[i].ID == item.ID {
			listedRow = &fabioList[i]
		}
	}
	if listedRow == nil || len(listedRow.SharedWith) != 1 || listedRow.SharedWith[0] != "mario" {
		t.Errorf("fabio's root listing row for the shared item: shared_with = %+v, want [mario]", listedRow)
	}

	marioGet := decodeJSON[apiItem](t, mustGet(t, ts, mario, "/api/v1/items/"+item.ID))
	if len(marioGet.SharedWith) != 0 {
		t.Errorf("mario's own GET of an item shared with him: shared_with = %+v, want empty (that's fabio's info to see, not his)", marioGet.SharedWith)
	}
}

func mustGet(t *testing.T, ts *testServer, user registeredUser, path string) *http.Response {
	t.Helper()
	res := authedRequest(t, http.MethodGet, ts.URL+path, user, nil)
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("GET %s: got status %d, body: %s", path, res.StatusCode, body)
	}
	return res
}

// TestUserShareFlow_ListMineCoversEveryItemNotJustOne covers GET
// /api/v1/user-shares — the "My shares" page's own listing of every
// direct grant the caller has made, across all of their items, as
// opposed to ListForItem's single-item view.
func TestUserShareFlow_ListMineCoversEveryItemNotJustOne(t *testing.T) {
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

	docOne := uploadFile(t, ts, fabio, nil, "one.txt", []byte("one"))
	docTwo := uploadFile(t, ts, fabio, nil, "two.txt", []byte("two"))

	if res := createUserShareWithPermission(t, ts, fabio, docOne.ID, mario.id, "view"); res.StatusCode != http.StatusCreated {
		t.Fatalf("share doc one with mario: got status %d", res.StatusCode)
	}
	if res := createUserShareWithPermission(t, ts, fabio, docTwo.ID, luigi.id, "edit"); res.StatusCode != http.StatusCreated {
		t.Fatalf("share doc two with luigi: got status %d", res.StatusCode)
	}

	mine := decodeJSON[[]userShareResponse](t, mustGet(t, ts, fabio, "/api/v1/user-shares"))
	if len(mine) != 2 {
		t.Fatalf("fabio's /api/v1/user-shares = %+v, want 2 grants", mine)
	}
	byItem := map[string]userShareResponse{}
	for _, g := range mine {
		byItem[g.ItemID] = g
	}
	if g, ok := byItem[docOne.ID]; !ok || g.SharedWithUsername != "mario" || g.Permission != "view" {
		t.Errorf("grant for doc one = %+v, want mario/view", g)
	}
	if g, ok := byItem[docTwo.ID]; !ok || g.SharedWithUsername != "luigi" || g.Permission != "edit" {
		t.Errorf("grant for doc two = %+v, want luigi/edit", g)
	}

	// mario and luigi each made no grants of their own — this is an
	// owner's-own listing, not "everything I can see".
	marioMine := decodeJSON[[]userShareResponse](t, mustGet(t, ts, mario, "/api/v1/user-shares"))
	if len(marioMine) != 0 {
		t.Errorf("mario's /api/v1/user-shares = %+v, want empty (he received a grant, didn't make one)", marioMine)
	}
}
