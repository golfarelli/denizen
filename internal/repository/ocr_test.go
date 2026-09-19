package repository

import (
	"context"
	"testing"

	"github.com/golfarelli/denizen/internal/db"
)

func TestOCRQueue_Semantics(t *testing.T) {
	ctx := context.Background()
	cn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cn.Close()

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := cn.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO users (id, username, password_hash, is_admin, quota_bytes, storage_used_bytes, disabled, created_at)
	      VALUES ('u1', 'alice', 'hash', 0, 0, 0, 0, 1)`)
	for i, id := range []string{"a", "b", "c"} {
		exec(`INSERT INTO items (id, owner_id, name, type, created_at, updated_at)
		      VALUES (?, 'u1', ?, 'file', ?, ?)`, id, id+".pdf", i+1, i+1)
	}

	r := NewOCRRepository(cn)
	pending := func() []string {
		t.Helper()
		items, err := r.ListPending(ctx, 10)
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]string, len(items))
		for i, it := range items {
			ids[i] = it.ID
		}
		return ids
	}
	same := func(got []string, want ...string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("got %v, want %v", got, want)
			}
		}
	}

	// Oldest-queued first, regardless of the order they were queued in
	// here relative to item creation.
	for _, q := range []struct {
		id string
		at int64
	}{{"c", 30}, {"a", 10}, {"b", 20}} {
		if err := r.SetQueued(ctx, q.id, true, q.at); err != nil {
			t.Fatal(err)
		}
	}
	same(pending(), "a", "b", "c")

	// Queueing again is a no-op, not a duplicate or a re-order.
	if err := r.SetQueued(ctx, "a", true, 99); err != nil {
		t.Fatal(err)
	}
	same(pending(), "a", "b", "c")

	// An attempt takes the file off the queue...
	if err := r.MarkAttempted(ctx, "a", 100); err != nil {
		t.Fatal(err)
	}
	same(pending(), "b", "c")

	// ...and it stays off: re-indexing an unchanged file must not resurrect
	// a scan the sweep already gave up on.
	if err := r.SetQueued(ctx, "a", true, 101); err != nil {
		t.Fatal(err)
	}
	same(pending(), "b", "c")

	// Replacing the bytes clears the marker, so it can be queued afresh.
	if err := r.ClearAttempt(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetQueued(ctx, "a", true, 102); err != nil {
		t.Fatal(err)
	}
	same(pending(), "b", "c", "a")

	// A replaced file that now has a real text layer no longer needs OCR.
	if err := r.SetQueued(ctx, "b", false, 103); err != nil {
		t.Fatal(err)
	}
	same(pending(), "c", "a")

	// Trashed items stay queued but aren't offered while trashed.
	exec(`UPDATE items SET deleted_at = 500 WHERE id = 'c'`)
	same(pending(), "a")
	exec(`UPDATE items SET deleted_at = NULL WHERE id = 'c'`)
	same(pending(), "c", "a")
}
