package flow

import (
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/golfarelli/denizen/internal/storage"
)

func TestTrashPurgeFlow_RemovesOnlyExpiredEntries(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()
	store := storage.New(ts.dataDir)

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	oldItem := uploadFile(t, ts, fabio, nil, "old.txt", []byte("should be purged"))
	recentItem := uploadFile(t, ts, fabio, nil, "recent.txt", []byte("should survive"))

	for _, id := range []string{oldItem.ID, recentItem.ID} {
		res := authedRequest(t, http.MethodDelete, ts.URL+"/api/v1/items/"+id, fabio, nil)
		if res.StatusCode != http.StatusNoContent {
			t.Fatalf("delete %s: got status %d", id, res.StatusCode)
		}
	}

	// Back-date oldItem's deleted_at directly in the DB to simulate it having
	// sat in the trash for 31 days — there's no way to fake the service's
	// clock from this package (it's a private field), so this is the
	// straightforward way to set up "already expired" for the test.
	longAgo := time.Now().Add(-31 * 24 * time.Hour).Unix()
	if _, err := ts.app.DB.ExecContext(ctx, `UPDATE items SET deleted_at = ? WHERE id = ?`, longAgo, oldItem.ID); err != nil {
		t.Fatalf("back-date oldItem.deleted_at: %v", err)
	}

	oldTrashPath := store.TrashPath(fabio.username, oldItem.ID, "old.txt")
	recentTrashPath := store.TrashPath(fabio.username, recentItem.ID, "recent.txt")
	mustExist(t, oldTrashPath)
	mustExist(t, recentTrashPath)

	purged, err := ts.app.Items.PurgeExpiredTrash(ctx, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("PurgeExpiredTrash: %v", err)
	}
	if purged != 1 {
		t.Errorf("purged = %d, want 1 (just old.txt)", purged)
	}

	// old.txt: gone from disk and from the database
	if _, err := os.Stat(oldTrashPath); !os.IsNotExist(err) {
		t.Errorf("old.txt should be gone from disk, stat returned: %v", err)
	}
	var count int
	if err := ts.app.DB.QueryRowContext(ctx, `SELECT count(*) FROM items WHERE id = ?`, oldItem.ID).Scan(&count); err != nil {
		t.Fatalf("count old item rows: %v", err)
	}
	if count != 0 {
		t.Error("old.txt's row is still in the database after purging")
	}

	// recent.txt: untouched
	mustExist(t, recentTrashPath)
	if err := ts.app.DB.QueryRowContext(ctx, `SELECT count(*) FROM items WHERE id = ?`, recentItem.ID).Scan(&count); err != nil {
		t.Fatalf("count recent item rows: %v", err)
	}
	if count != 1 {
		t.Error("recent.txt's row should still be in the database — it hasn't expired yet")
	}
}
