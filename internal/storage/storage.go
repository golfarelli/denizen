// Package storage performs the real filesystem operations backing a user's
// files — creating folders, moving things to and from trash, removing them
// for good — following the layout in docs/ARCHITECTURE.md:
//
//	<dataDir>/users/<username>/files/   mirrors the user's visible folder tree
//	<dataDir>/users/<username>/.trash/  flat, holds soft-deleted items
//
// This package only ever moves bytes around on disk for a path it's given;
// it has no idea what an "item" is. Resolving an item's ID to the path
// where its bytes live is internal/service's job (it needs the DB to walk
// parent_id up to the root) — see internal/service/item.go.
package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type Store struct {
	dataDir string
}

func New(dataDir string) *Store {
	return &Store{dataDir: dataDir}
}

// UserFilesRoot is the root of username's visible folder tree.
func (s *Store) UserFilesRoot(username string) string {
	return filepath.Join(s.dataDir, "users", username, "files")
}

// UserTrashRoot is where username's soft-deleted items live.
func (s *Store) UserTrashRoot(username string) string {
	return filepath.Join(s.dataDir, "users", username, ".trash")
}

// StagingRoot is where in-progress resumable (tus) uploads are written
// before they're finalized into a user's folder tree — see
// internal/upload. It must be on the same filesystem as UserFilesRoot for
// every user, since finalizing an upload is a Rename, not a copy.
func (s *Store) StagingRoot() string {
	return filepath.Join(s.dataDir, "staging", "uploads")
}

// TrashPath returns the path a trashed item with the given id and original
// name lives at — flat, prefixed by id, so a name collision with something
// else already in (or later put into) the trash is impossible. See
// docs/ARCHITECTURE.md's storage layout for why trash isn't nested.
func (s *Store) TrashPath(username, itemID, originalName string) string {
	return filepath.Join(s.UserTrashRoot(username), fmt.Sprintf("%s_%s", itemID, originalName))
}

// CreateFolder creates path and any missing parent directories.
func (s *Store) CreateFolder(path string) error {
	return os.MkdirAll(path, 0o755)
}

// Rename moves oldPath to newPath — cheap (metadata-only on the same
// filesystem, not a recursive copy) whether it's a file or a whole folder
// subtree. Creates newPath's parent directory first, in case a move also
// changes the parent.
func (s *Store) Rename(oldPath, newPath string) error {
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		return err
	}
	return os.Rename(oldPath, newPath)
}

// Remove deletes path (and, if it's a folder, everything under it) for
// good. Used only for permanent trash deletion — see internal/service.
func (s *Store) Remove(path string) error {
	return os.RemoveAll(path)
}

// Exists reports whether path already exists on disk.
func (s *Store) Exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// Checksum returns the hex-encoded SHA-256 of the file at path — used to
// record a file item's checksum once its bytes are at rest (see
// internal/service.FinalizeUpload).
func (s *Store) Checksum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// CopyFile duplicates the file at src to dst as independent bytes — a real
// copy, not a hard link, since trashing or overwriting one of the two must
// never affect the other.
func (s *Store) CopyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst) // don't leave a partial file behind
		return err
	}
	return nil
}

// FreeBytes reports the actual free space on the filesystem backing the
// data directory — a safety net checked independently of the logical
// per-user quota (users.quota_bytes), since quotas assigned to different
// users can add up to more than the disk actually has. Linux-only (this is
// always a container running on Linux — see docs/ARCHITECTURE.md), via
// golang.org/x/sys/unix rather than a new dependency: it's already pulled
// in transitively by the SQLite driver.
func (s *Store) FreeBytes() (uint64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(s.dataDir, &stat); err != nil {
		return 0, fmt.Errorf("storage: statfs %s: %w", s.dataDir, err)
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}
