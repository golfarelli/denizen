package upload

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CollectGarbage removes uploads left in stagingDir that haven't been
// written to in at least olderThan — started (by a POST) but never
// finished (no completing PATCH), and abandoned: the client gave up, lost
// its network connection for good, or never came back. tusd's filestore
// doesn't clean these up on its own (see its package doc), so nothing
// else does either unless this runs.
//
// Age is judged by each upload's `.info` file mtime, which filestore
// touches on every write — not by an upload metadata field, since FileInfo
// doesn't carry a creation or last-written timestamp of its own. A
// still-in-progress upload keeps getting recent writes and is never a
// candidate; this only ever catches uploads nobody has touched in a while.
func CollectGarbage(stagingDir string, olderThan time.Duration) (removed int, err error) {
	entries, err := os.ReadDir(stagingDir)
	if os.IsNotExist(err) {
		return 0, nil // nothing has ever been uploaded here yet
	}
	if err != nil {
		return 0, err
	}

	cutoff := time.Now().Add(-olderThan)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".info") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue // e.g. removed concurrently — nothing to do about it
		}
		if info.ModTime().After(cutoff) {
			continue // recently active, not abandoned
		}

		id := strings.TrimSuffix(entry.Name(), ".info")
		os.Remove(filepath.Join(stagingDir, entry.Name())) // .info
		os.Remove(filepath.Join(stagingDir, id))           // the binary data file, if any bytes were written yet
		removed++
	}
	return removed, nil
}
