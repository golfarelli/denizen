package flow

import (
	"net/http"
	"testing"
	"time"
)

func listRecent(t *testing.T, ts *testServer, user registeredUser) []apiItem {
	t.Helper()
	res := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/recent", user, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/v1/recent: got status %d", res.StatusCode)
	}
	return decodeJSON[[]apiItem](t, res)
}

// TestRecentFlow_MostRecentFirstFoldersExcludedOwnerScoped covers
// ItemRepository.ListRecentFiles' three defining behaviors in one pass:
// ordering, the folders-excluded filter, and per-owner scoping.
func TestRecentFlow_MostRecentFirstFoldersExcludedOwnerScoped(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	folderRes := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items", fabio, map[string]any{
		"type": "folder", "name": "Bollette", "parent_id": nil,
	})
	folder := decodeJSON[apiItem](t, folderRes)

	older := uploadFile(t, ts, fabio, nil, "older.txt", []byte("first"))
	newer := uploadFile(t, ts, fabio, &folder.ID, "newer.txt", []byte("second, nested in a folder"))
	// updated_at is second-resolution and this whole upload sequence runs
	// within one wall-clock second — same fix thumbnail_flow_test.go's own
	// cache-invalidation test needed, for the same underlying reason.
	if _, err := ts.app.DB.ExecContext(ctx, `UPDATE items SET updated_at = updated_at + 1 WHERE id = ?`, newer.ID); err != nil {
		t.Fatalf("bump newer.updated_at: %v", err)
	}

	// A second user's own file must never show up in fabio's Recent.
	secondCode, _, err := ts.app.Auth.CreateInvite(ctx, fabio.id, nil, time.Hour)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	mario := registerAndLogin(t, ts, secondCode, "mario", "another-strong-password")
	uploadFile(t, ts, mario, nil, "mario-only.txt", []byte("not fabio's"))

	recent := listRecent(t, ts, fabio)

	if len(recent) != 2 {
		t.Fatalf("recent = %d items, want exactly 2 (the folder excluded, mario's file not fabio's)", len(recent))
	}
	if recent[0].ID != newer.ID || recent[1].ID != older.ID {
		t.Errorf("recent order = [%s, %s], want [newer, older] (most recently modified first)", recent[0].Name, recent[1].Name)
	}
	for _, item := range recent {
		if item.Type != "file" {
			t.Errorf("recent contains a %s (%s) — folders should never appear", item.Type, item.Name)
		}
		if item.ID == folder.ID {
			t.Error("recent includes the folder itself")
		}
	}
}

func TestRecentFlow_TrashedFileIsExcluded(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	item := uploadFile(t, ts, fabio, nil, "doomed.txt", []byte("about to be trashed"))
	deleteRes := authedRequest(t, http.MethodDelete, ts.URL+"/api/v1/items/"+item.ID, fabio, nil)
	if deleteRes.StatusCode != http.StatusNoContent {
		t.Fatalf("trash the item: got status %d", deleteRes.StatusCode)
	}

	recent := listRecent(t, ts, fabio)
	for _, r := range recent {
		if r.ID == item.ID {
			t.Error("recent includes a trashed item")
		}
	}
}
