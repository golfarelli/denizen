package flow

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golfarelli/denizen/internal/storage"
)

type apiItem struct {
	ID        string  `json:"id"`
	ParentID  *string `json:"parent_id"`
	Name      string  `json:"name"`
	Type      string  `json:"type"`
	SizeBytes int64   `json:"size_bytes"`
	CreatedAt int64   `json:"created_at"`
	UpdatedAt int64   `json:"updated_at"`
	DeletedAt *int64  `json:"deleted_at,omitempty"`
}

type registeredUser struct {
	id          string
	username    string
	accessToken string
}

func registerAndLogin(t *testing.T, ts *testServer, inviteCode, username, password string) registeredUser {
	t.Helper()

	registerRes := postJSON(t, ts.URL+"/api/v1/auth/register", map[string]string{
		"invite_code": inviteCode,
		"username":    username,
		"password":    password,
	})
	if registerRes.StatusCode != http.StatusCreated {
		t.Fatalf("register %q: got status %d", username, registerRes.StatusCode)
	}
	registered := decodeJSON[registerResponse](t, registerRes)

	loginRes := postJSON(t, ts.URL+"/api/v1/auth/login", map[string]string{
		"username": username,
		"password": password,
	})
	if loginRes.StatusCode != http.StatusOK {
		t.Fatalf("login %q: got status %d", username, loginRes.StatusCode)
	}
	tokens := decodeJSON[tokenPairResponse](t, loginRes)

	return registeredUser{id: registered.ID, username: username, accessToken: tokens.AccessToken}
}

// authedRequest issues method to url as user, JSON-encoding body if it's
// non-nil (pass nil for GET/DELETE with no body).
func authedRequest(t *testing.T, method, url string, user registeredUser, body any) *http.Response {
	t.Helper()

	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, url, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+user.accessToken)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	return res
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected %s to exist on disk, but: %v", path, err)
	}
}

func mustNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected %s to be gone from disk, but stat returned: %v", path, err)
	}
}

func TestItemsFlow_FoldersCreateListMoveTrashRestoreDelete(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()
	store := storage.New(ts.dataDir)

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")
	filesRoot := store.UserFilesRoot(fabio.username)

	// --- create a root folder, verify DB row and real directory -------------
	createDocsRes := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items", fabio, map[string]any{
		"type": "folder", "name": "Documents", "parent_id": nil,
	})
	if createDocsRes.StatusCode != http.StatusCreated {
		t.Fatalf("create Documents: got status %d", createDocsRes.StatusCode)
	}
	docs := decodeJSON[apiItem](t, createDocsRes)
	if docs.Name != "Documents" || docs.Type != "folder" || docs.ParentID != nil {
		t.Errorf("Documents = %+v, want name=Documents type=folder parent_id=nil", docs)
	}
	mustExist(t, filepath.Join(filesRoot, "Documents"))

	var dbName, dbType string
	var dbParentID any
	if err := ts.app.DB.QueryRowContext(ctx, `SELECT name, type, parent_id FROM items WHERE id = ?`, docs.ID).
		Scan(&dbName, &dbType, &dbParentID); err != nil {
		t.Fatalf("scan Documents row: %v", err)
	}
	if dbName != "Documents" || dbType != "folder" || dbParentID != nil {
		t.Errorf("Documents DB row = {name:%q type:%q parent_id:%v}, want {Documents folder <nil>}", dbName, dbType, dbParentID)
	}

	// --- a nested folder lands on disk inside its parent ---------------------
	createReportsRes := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items", fabio, map[string]any{
		"type": "folder", "name": "Reports", "parent_id": docs.ID,
	})
	if createReportsRes.StatusCode != http.StatusCreated {
		t.Fatalf("create Reports: got status %d", createReportsRes.StatusCode)
	}
	reports := decodeJSON[apiItem](t, createReportsRes)
	mustExist(t, filepath.Join(filesRoot, "Documents", "Reports"))

	// --- a name collision at the same level gets auto-suffixed ---------------
	createDupRes := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items", fabio, map[string]any{
		"type": "folder", "name": "Documents", "parent_id": nil,
	})
	if createDupRes.StatusCode != http.StatusCreated {
		t.Fatalf("create duplicate Documents: got status %d", createDupRes.StatusCode)
	}
	dup := decodeJSON[apiItem](t, createDupRes)
	if dup.Name != "Documents (1)" {
		t.Errorf("dup.Name = %q, want %q", dup.Name, "Documents (1)")
	}
	mustExist(t, filepath.Join(filesRoot, "Documents (1)"))

	// --- listing ---------------------------------------------------------------
	listRootRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items", fabio, nil)
	rootItems := decodeJSON[[]apiItem](t, listRootRes)
	if len(rootItems) != 2 {
		t.Fatalf("root listing has %d items, want 2 (Documents, Documents (1))", len(rootItems))
	}

	listDocsRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items?parent_id="+docs.ID, fabio, nil)
	docsChildren := decodeJSON[[]apiItem](t, listDocsRes)
	if len(docsChildren) != 1 || docsChildren[0].ID != reports.ID {
		t.Fatalf("Documents children = %+v, want just Reports", docsChildren)
	}

	// --- move: rename Reports to Archives and move it to root ------------------
	moveRes := authedRequest(t, http.MethodPatch, ts.URL+"/api/v1/items/"+reports.ID, fabio, map[string]any{
		"name": "Archives", "parent_id": nil,
	})
	if moveRes.StatusCode != http.StatusOK {
		t.Fatalf("move Reports->Archives: got status %d", moveRes.StatusCode)
	}
	archives := decodeJSON[apiItem](t, moveRes)
	if archives.Name != "Archives" || archives.ParentID != nil {
		t.Errorf("archives = %+v, want name=Archives parent_id=nil", archives)
	}
	mustNotExist(t, filepath.Join(filesRoot, "Documents", "Reports"))
	mustExist(t, filepath.Join(filesRoot, "Archives"))

	// --- a folder cannot be moved into itself -----------------------------------
	selfMoveRes := authedRequest(t, http.MethodPatch, ts.URL+"/api/v1/items/"+docs.ID, fabio, map[string]any{
		"name": "Documents", "parent_id": docs.ID,
	})
	if selfMoveRes.StatusCode != http.StatusBadRequest {
		t.Errorf("moving Documents into itself: got status %d, want %d", selfMoveRes.StatusCode, http.StatusBadRequest)
	}

	// --- soft delete: real directory ends up in .trash, DB row marked ----------
	deleteRes := authedRequest(t, http.MethodDelete, ts.URL+"/api/v1/items/"+archives.ID, fabio, nil)
	if deleteRes.StatusCode != http.StatusNoContent {
		t.Fatalf("delete Archives: got status %d", deleteRes.StatusCode)
	}
	mustNotExist(t, filepath.Join(filesRoot, "Archives"))
	trashedPath := store.TrashPath(fabio.username, archives.ID, "Archives")
	mustExist(t, trashedPath)

	var deletedAt any
	if err := ts.app.DB.QueryRowContext(ctx, `SELECT deleted_at FROM items WHERE id = ?`, archives.ID).Scan(&deletedAt); err != nil {
		t.Fatalf("scan Archives deleted_at: %v", err)
	}
	if deletedAt == nil {
		t.Error("Archives.deleted_at is NULL after delete, want it set")
	}

	// a trashed item no longer shows up as an active item
	getDeletedRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+archives.ID, fabio, nil)
	if getDeletedRes.StatusCode != http.StatusNotFound {
		t.Errorf("GET a trashed item: got status %d, want %d", getDeletedRes.StatusCode, http.StatusNotFound)
	}

	// --- trash listing -----------------------------------------------------------
	listTrashRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/trash", fabio, nil)
	trash := decodeJSON[[]apiItem](t, listTrashRes)
	if len(trash) != 1 || trash[0].ID != archives.ID {
		t.Fatalf("trash listing = %+v, want just Archives", trash)
	}

	// --- restore: real directory reappears where it was, DB row cleared --------
	restoreRes := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items/"+archives.ID+"/restore", fabio, nil)
	if restoreRes.StatusCode != http.StatusOK {
		t.Fatalf("restore Archives: got status %d", restoreRes.StatusCode)
	}
	restored := decodeJSON[apiItem](t, restoreRes)
	if restored.DeletedAt != nil {
		t.Errorf("restored.DeletedAt = %v, want nil", restored.DeletedAt)
	}
	mustNotExist(t, trashedPath)
	mustExist(t, filepath.Join(filesRoot, "Archives"))

	// --- permanent delete: gone from disk, gone from the database --------------
	deleteAgainRes := authedRequest(t, http.MethodDelete, ts.URL+"/api/v1/items/"+archives.ID, fabio, nil)
	if deleteAgainRes.StatusCode != http.StatusNoContent {
		t.Fatalf("re-delete Archives: got status %d", deleteAgainRes.StatusCode)
	}
	permanentRes := authedRequest(t, http.MethodDelete, ts.URL+"/api/v1/trash/"+archives.ID, fabio, nil)
	if permanentRes.StatusCode != http.StatusNoContent {
		t.Fatalf("permanently delete Archives: got status %d", permanentRes.StatusCode)
	}
	mustNotExist(t, trashedPath)

	var count int
	if err := ts.app.DB.QueryRowContext(ctx, `SELECT count(*) FROM items WHERE id = ?`, archives.ID).Scan(&count); err != nil {
		t.Fatalf("count Archives rows: %v", err)
	}
	if count != 0 {
		t.Errorf("Archives row still present after permanent delete (count=%d)", count)
	}
}

func TestItemsFlow_UsersCannotSeeEachOthersItems(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	bootstrapCode, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", bootstrapCode, created, err)
	}
	fabio := registerAndLogin(t, ts, bootstrapCode, "fabio", "correct-horse-battery-staple")

	secondCode, _, err := ts.app.Auth.CreateInvite(ctx, fabio.id, nil, time.Hour)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	mario := registerAndLogin(t, ts, secondCode, "mario", "another-strong-password")

	createRes := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items", fabio, map[string]any{
		"type": "folder", "name": "Private", "parent_id": nil,
	})
	if createRes.StatusCode != http.StatusCreated {
		t.Fatalf("create Private: got status %d", createRes.StatusCode)
	}
	private := decodeJSON[apiItem](t, createRes)

	getRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+private.ID, mario, nil)
	if getRes.StatusCode != http.StatusNotFound {
		t.Errorf("mario reading fabio's folder: got status %d, want %d (not found, not forbidden — existence shouldn't leak)",
			getRes.StatusCode, http.StatusNotFound)
	}

	deleteRes := authedRequest(t, http.MethodDelete, ts.URL+"/api/v1/items/"+private.ID, mario, nil)
	if deleteRes.StatusCode != http.StatusNotFound {
		t.Errorf("mario deleting fabio's folder: got status %d, want %d", deleteRes.StatusCode, http.StatusNotFound)
	}

	listRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items", mario, nil)
	marioItems := decodeJSON[[]apiItem](t, listRes)
	if len(marioItems) != 0 {
		t.Errorf("mario's root listing = %+v, want empty (fabio's items must not leak in)", marioItems)
	}
}

func TestItemsFlow_RejectsUnsafeNames(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	cases := []string{"", ".", "..", "a/b", "with/slash"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			res := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items", fabio, map[string]any{
				"type": "folder", "name": name, "parent_id": nil,
			})
			if res.StatusCode != http.StatusBadRequest {
				t.Errorf("name %q: got status %d, want %d", name, res.StatusCode, http.StatusBadRequest)
			}
		})
	}
}
