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
	"fmt"
	"os"
	"path/filepath"
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
