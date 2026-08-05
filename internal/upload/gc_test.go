package upload

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func touch(t *testing.T, path string, mtime time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

func TestCollectGarbage_RemovesOnlyStaleUploads(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	// an abandoned upload: both files old
	touch(t, filepath.Join(dir, "stale-id.info"), now.Add(-48*time.Hour))
	touch(t, filepath.Join(dir, "stale-id"), now.Add(-48*time.Hour))

	// a fresh, in-progress upload: recently written, must survive
	touch(t, filepath.Join(dir, "fresh-id.info"), now)
	touch(t, filepath.Join(dir, "fresh-id"), now)

	// an old upload that never got any bytes at all (no binary file yet) —
	// still garbage, must not make CollectGarbage choke on the missing file
	touch(t, filepath.Join(dir, "empty-id.info"), now.Add(-48*time.Hour))

	removed, err := CollectGarbage(dir, 24*time.Hour)
	if err != nil {
		t.Fatalf("CollectGarbage: %v", err)
	}
	if removed != 2 {
		t.Errorf("removed = %d, want 2 (stale-id, empty-id)", removed)
	}

	for _, name := range []string{"stale-id.info", "stale-id", "empty-id.info"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s should have been removed, stat returned: %v", name, err)
		}
	}
	for _, name := range []string{"fresh-id.info", "fresh-id"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s should have survived, but: %v", name, err)
		}
	}
}

func TestCollectGarbage_MissingDirectoryIsNotAnError(t *testing.T) {
	removed, err := CollectGarbage(filepath.Join(t.TempDir(), "does-not-exist"), time.Hour)
	if err != nil {
		t.Fatalf("CollectGarbage on a missing dir: %v", err)
	}
	if removed != 0 {
		t.Errorf("removed = %d, want 0", removed)
	}
}
