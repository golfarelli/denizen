package flow

import (
	"net/http"
	"testing"
	"time"
)

func addFavorite(t *testing.T, ts *testServer, user registeredUser, itemID string) *http.Response {
	t.Helper()
	return authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items/"+itemID+"/favorite", user, map[string]any{})
}

func removeFavorite(t *testing.T, ts *testServer, user registeredUser, itemID string) *http.Response {
	t.Helper()
	return authedRequest(t, http.MethodDelete, ts.URL+"/api/v1/items/"+itemID+"/favorite", user, nil)
}

func listFavorites(t *testing.T, ts *testServer, user registeredUser) []apiItem {
	t.Helper()
	res := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/favorites", user, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/v1/favorites: got status %d", res.StatusCode)
	}
	return decodeJSON[[]apiItem](t, res)
}

func TestFavoriteFlow_AddListRemove(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	item := uploadFile(t, ts, fabio, nil, "favorite-me.txt", []byte("star this"))

	if res := addFavorite(t, ts, fabio, item.ID); res.StatusCode != http.StatusNoContent {
		t.Fatalf("add favorite: got status %d", res.StatusCode)
	}

	favorites := listFavorites(t, ts, fabio)
	if len(favorites) != 1 || favorites[0].ID != item.ID {
		t.Fatalf("favorites = %+v, want exactly [%s]", favorites, item.ID)
	}
	if !favorites[0].IsFavorite {
		t.Error("listed favorite has is_favorite = false")
	}

	// Adding it again is a harmless no-op, not an error or a duplicate row.
	if res := addFavorite(t, ts, fabio, item.ID); res.StatusCode != http.StatusNoContent {
		t.Fatalf("re-add favorite: got status %d", res.StatusCode)
	}
	if favorites := listFavorites(t, ts, fabio); len(favorites) != 1 {
		t.Errorf("favorites after re-adding = %d, want still 1 (no duplicate)", len(favorites))
	}

	// The regular listing also reports is_favorite now.
	listRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items", fabio, nil)
	list := decodeJSON[[]apiItem](t, listRes)
	if len(list) != 1 || !list[0].IsFavorite {
		t.Errorf("GET /items IsFavorite = %+v, want true on %s", list, item.ID)
	}

	if res := removeFavorite(t, ts, fabio, item.ID); res.StatusCode != http.StatusNoContent {
		t.Fatalf("remove favorite: got status %d", res.StatusCode)
	}
	if favorites := listFavorites(t, ts, fabio); len(favorites) != 0 {
		t.Errorf("favorites after removing = %d, want 0", len(favorites))
	}

	// Removing again (already gone) is also a harmless no-op.
	if res := removeFavorite(t, ts, fabio, item.ID); res.StatusCode != http.StatusNoContent {
		t.Errorf("re-remove favorite: got status %d, want 204", res.StatusCode)
	}
}

// TestFavoriteFlow_SharedItemIncludedUntilRevoked covers the explicit
// scope decision (secondbrain session 2026-08-16): favoriting isn't
// limited to your own items, and a favorite silently stops resolving (not
// an error) once the underlying share is revoked, same as everywhere else
// a revoked grant just makes something disappear rather than erroring.
func TestFavoriteFlow_SharedItemIncludedUntilRevoked(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	secondCode, _, err := ts.app.Auth.CreateInvite(ctx, fabio.id, nil, time.Hour)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	mario := registerAndLogin(t, ts, secondCode, "mario", "another-strong-password")

	item := uploadFile(t, ts, fabio, nil, "shared-with-mario.txt", []byte("shared content"))
	grantRes := createUserShare(t, ts, fabio, item.ID, mario.id)
	if grantRes.StatusCode != http.StatusCreated {
		t.Fatalf("create user share: got status %d", grantRes.StatusCode)
	}
	grant := decodeJSON[userShareResponse](t, grantRes)

	if res := addFavorite(t, ts, mario, item.ID); res.StatusCode != http.StatusNoContent {
		t.Fatalf("mario favorites fabio's shared item: got status %d", res.StatusCode)
	}
	favorites := listFavorites(t, ts, mario)
	if len(favorites) != 1 || favorites[0].ID != item.ID || favorites[0].Owned {
		t.Fatalf("mario's favorites = %+v, want exactly [%s] with owned=false", favorites, item.ID)
	}

	revokeRes := authedRequest(t, http.MethodDelete, ts.URL+"/api/v1/user-shares/"+grant.ID, fabio, nil)
	if revokeRes.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke share: got status %d", revokeRes.StatusCode)
	}

	if favorites := listFavorites(t, ts, mario); len(favorites) != 0 {
		t.Errorf("mario's favorites after revoke = %+v, want empty (item no longer accessible, not an error)", favorites)
	}
}

func TestFavoriteFlow_CannotFavoriteSomeoneElsesPrivateItem(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	secondCode, _, err := ts.app.Auth.CreateInvite(ctx, fabio.id, nil, time.Hour)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	mario := registerAndLogin(t, ts, secondCode, "mario", "another-strong-password")

	item := uploadFile(t, ts, fabio, nil, "private.txt", []byte("not shared with anyone"))

	if res := addFavorite(t, ts, mario, item.ID); res.StatusCode != http.StatusNotFound {
		t.Errorf("mario favoriting fabio's private item: got status %d, want %d", res.StatusCode, http.StatusNotFound)
	}
}
