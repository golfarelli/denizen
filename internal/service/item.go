package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/golfarelli/denizen/internal/apperr"
	"github.com/golfarelli/denizen/internal/idgen"
	"github.com/golfarelli/denizen/internal/model"
	"github.com/golfarelli/denizen/internal/repository"
	"github.com/golfarelli/denizen/internal/storage"
)

// ItemService covers folders and (once upload lands) files: creating,
// listing, moving/renaming, and trash (soft delete, restore, permanent
// delete). It's also the only layer that knows an item's ID maps to a real
// path on disk — see pathOf/folderPath below.
type ItemService struct {
	items   *repository.ItemRepository
	users   *repository.UserRepository
	storage *storage.Store
	now     func() time.Time // swappable in tests; defaults to time.Now
}

func NewItemService(items *repository.ItemRepository, users *repository.UserRepository, store *storage.Store) *ItemService {
	return &ItemService{items: items, users: users, storage: store, now: time.Now}
}

// --- name/path helpers -----------------------------------------------------

// validateName rejects anything that isn't safe to use as a single path
// segment — in particular "." and ".." and "/", which would otherwise let a
// crafted name escape the user's own folder tree once it reaches
// filepath.Join.
func validateName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return apperr.Validation("name is required")
	}
	if name == "." || name == ".." {
		return apperr.Validation("invalid name")
	}
	if strings.ContainsAny(name, "/\x00") {
		return apperr.Validation("name cannot contain '/'")
	}
	return nil
}

// uniqueName finds a name that doesn't collide with an existing active
// sibling under parentID, auto-suffixing like a desktop file manager
// ("file (1).pdf") if it does. excludeID excludes the item being
// renamed/moved/restored from the collision check — pass "" when creating
// something brand new.
func (s *ItemService) uniqueName(ctx context.Context, ownerID string, parentID *string, itemType model.ItemType, name, excludeID string) (string, error) {
	base, ext := name, ""
	if itemType == model.ItemTypeFile {
		ext = filepath.Ext(name)
		base = strings.TrimSuffix(name, ext)
	}
	candidate := name
	const attemptLimit = 10000
	for i := 1; i <= attemptLimit; i++ {
		exists, err := s.items.NameExists(ctx, ownerID, parentID, candidate, excludeID)
		if err != nil {
			return "", err
		}
		if !exists {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s (%d)%s", base, i, ext)
	}
	return "", apperr.Internal
}

// pathOf resolves the real on-disk path of an existing item by walking its
// ancestor chain up to the user's root.
func (s *ItemService) pathOf(ctx context.Context, item *model.Item, username string) (string, error) {
	if item.ParentID == nil {
		return filepath.Join(s.storage.UserFilesRoot(username), item.Name), nil
	}
	parent, err := s.items.GetByID(ctx, *item.ParentID)
	if err != nil {
		return "", err
	}
	parentPath, err := s.pathOf(ctx, parent, username)
	if err != nil {
		return "", err
	}
	return filepath.Join(parentPath, item.Name), nil
}

// folderPath resolves the real on-disk path of the folder identified by
// parentID (nil = the user's root), checking along the way that it exists,
// belongs to ownerID, is actually a folder, and isn't itself in the trash.
func (s *ItemService) folderPath(ctx context.Context, ownerID, username string, parentID *string) (string, error) {
	if parentID == nil {
		return s.storage.UserFilesRoot(username), nil
	}
	parent, err := s.items.GetByID(ctx, *parentID)
	if err != nil {
		if err == repository.ErrNotFound {
			return "", apperr.Validation("parent folder not found")
		}
		return "", err
	}
	if parent.OwnerID != ownerID {
		return "", apperr.Validation("parent folder not found")
	}
	if parent.DeletedAt != nil {
		return "", apperr.Validation("parent folder is in trash")
	}
	if parent.Type != model.ItemTypeFolder {
		return "", apperr.Validation("parent is not a folder")
	}
	return s.pathOf(ctx, parent, username)
}

func (s *ItemService) username(ctx context.Context, ownerID string) (string, error) {
	user, err := s.users.GetByID(ctx, ownerID)
	if err != nil {
		return "", err
	}
	return user.Username, nil
}

// --- read ---------------------------------------------------------------

// Get returns an active (non-trashed) item owned by ownerID. Returns
// apperr.NotFound — not apperr.Forbidden — when id belongs to someone else,
// so a caller can't tell "not yours" apart from "doesn't exist".
func (s *ItemService) Get(ctx context.Context, ownerID, id string) (*model.Item, error) {
	item, err := s.items.GetByID(ctx, id)
	if err != nil {
		if err == repository.ErrNotFound {
			return nil, apperr.NotFound
		}
		return nil, err
	}
	if item.OwnerID != ownerID || item.DeletedAt != nil {
		return nil, apperr.NotFound
	}
	return item, nil
}

// ListChildren lists the active direct children of parentID (nil = root).
func (s *ItemService) ListChildren(ctx context.Context, ownerID string, parentID *string) ([]*model.Item, error) {
	if parentID != nil {
		parent, err := s.Get(ctx, ownerID, *parentID)
		if err != nil {
			return nil, err
		}
		if parent.Type != model.ItemTypeFolder {
			return nil, apperr.Validation("not a folder")
		}
	}
	return s.items.ListChildren(ctx, ownerID, parentID)
}

// ListTrash lists only the top-level entry of each trashed subtree — e.g.
// deleting a folder with files in it shows one row (the folder), not one
// per file, matching what a user actually did.
func (s *ItemService) ListTrash(ctx context.Context, ownerID string) ([]*model.Item, error) {
	all, err := s.items.ListTrash(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	trashed := make(map[string]bool, len(all))
	for _, item := range all {
		trashed[item.ID] = true
	}
	var top []*model.Item
	for _, item := range all {
		if item.ParentID == nil || !trashed[*item.ParentID] {
			top = append(top, item)
		}
	}
	return top, nil
}

// --- create ---------------------------------------------------------------

// CreateFolder creates a new folder under parentID (nil = root). Files
// aren't created through this — they arrive via the (upcoming) upload
// endpoint instead, since a file needs bytes, not just a name.
func (s *ItemService) CreateFolder(ctx context.Context, ownerID string, parentID *string, name string) (*model.Item, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)

	username, err := s.username(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	folderPath, err := s.folderPath(ctx, ownerID, username, parentID)
	if err != nil {
		return nil, err
	}
	finalName, err := s.uniqueName(ctx, ownerID, parentID, model.ItemTypeFolder, name, "")
	if err != nil {
		return nil, err
	}

	// Disk first, then the DB row — if the DB insert fails after the
	// directory was created, we clean it up rather than leave an orphan
	// directory a retry would collide with.
	targetPath := filepath.Join(folderPath, finalName)
	if err := s.storage.CreateFolder(targetPath); err != nil {
		return nil, err
	}

	now := s.now().Unix()
	item := &model.Item{
		ID:        idgen.New(),
		OwnerID:   ownerID,
		ParentID:  parentID,
		Name:      finalName,
		Type:      model.ItemTypeFolder,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.items.Create(ctx, item); err != nil {
		_ = s.storage.Remove(targetPath)
		return nil, err
	}
	return item, nil
}

// --- move / rename ----------------------------------------------------------

// MoveInput is a full replacement of an item's name and location — like the
// rest of this codebase's PATCH endpoints, both fields are always supplied
// by the caller, not merged in partially.
type MoveInput struct {
	Name     string
	ParentID *string // nil = the user's root
}

// Move renames and/or reparents an item, moving its bytes on disk to match.
func (s *ItemService) Move(ctx context.Context, ownerID, id string, in MoveInput) (*model.Item, error) {
	if err := validateName(in.Name); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.Name)

	item, err := s.Get(ctx, ownerID, id)
	if err != nil {
		return nil, err
	}

	if in.ParentID != nil && item.Type == model.ItemTypeFolder {
		if err := s.checkNotSelfOrDescendant(ctx, item.ID, *in.ParentID); err != nil {
			return nil, err
		}
	}

	username, err := s.username(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	oldPath, err := s.pathOf(ctx, item, username)
	if err != nil {
		return nil, err
	}
	newFolderPath, err := s.folderPath(ctx, ownerID, username, in.ParentID)
	if err != nil {
		return nil, err
	}
	finalName, err := s.uniqueName(ctx, ownerID, in.ParentID, item.Type, name, item.ID)
	if err != nil {
		return nil, err
	}
	newPath := filepath.Join(newFolderPath, finalName)

	if newPath != oldPath {
		if err := s.storage.Rename(oldPath, newPath); err != nil {
			return nil, err
		}
	}

	now := s.now().Unix()
	if err := s.items.UpdateNameParent(ctx, id, finalName, in.ParentID, now); err != nil {
		if newPath != oldPath {
			_ = s.storage.Rename(newPath, oldPath) // best-effort: keep DB and disk in agreement
		}
		return nil, err
	}

	item.Name = finalName
	item.ParentID = in.ParentID
	item.UpdatedAt = now
	return item, nil
}

// checkNotSelfOrDescendant rejects moving a folder into itself or into one
// of its own descendants, which would either be a no-op cycle or orphan the
// folder (and everything in it) from the tree entirely.
func (s *ItemService) checkNotSelfOrDescendant(ctx context.Context, itemID, targetParentID string) error {
	current := targetParentID
	for {
		if current == itemID {
			return apperr.Validation("cannot move a folder into itself or one of its own subfolders")
		}
		node, err := s.items.GetByID(ctx, current)
		if err != nil {
			if err == repository.ErrNotFound {
				return apperr.Validation("parent folder not found")
			}
			return err
		}
		if node.ParentID == nil {
			return nil
		}
		current = *node.ParentID
	}
}

// --- trash ------------------------------------------------------------------

// Delete moves item — and, if it's a folder, everything under it — to
// trash. The whole subtree moves as a single, cheap filesystem rename
// (metadata-only, not a recursive copy), then every row in it is marked
// deleted_at so the database agrees with what's now sitting in .trash/.
//
// Known limitation: this walks *active* children to mark them, so an item
// that was already individually trashed before its ancestor is deleted
// (and is therefore no longer physically nested under it) isn't visited
// again here — a narrow edge case, not handled specially in this pass.
func (s *ItemService) Delete(ctx context.Context, ownerID, id string) error {
	item, err := s.Get(ctx, ownerID, id)
	if err != nil {
		return err
	}
	username, err := s.username(ctx, ownerID)
	if err != nil {
		return err
	}
	itemPath, err := s.pathOf(ctx, item, username)
	if err != nil {
		return err
	}
	trashPath := s.storage.TrashPath(username, item.ID, item.Name)

	if err := s.storage.Rename(itemPath, trashPath); err != nil {
		return err
	}

	now := s.now().Unix()
	if err := s.markSubtreeDeleted(ctx, ownerID, item.ID, now); err != nil {
		_ = s.storage.Rename(trashPath, itemPath) // best-effort: keep DB and disk in agreement
		return err
	}
	return nil
}

func (s *ItemService) markSubtreeDeleted(ctx context.Context, ownerID, id string, deletedAt int64) error {
	if err := s.items.SoftDelete(ctx, id, deletedAt); err != nil {
		return err
	}
	children, err := s.items.ListChildren(ctx, ownerID, &id)
	if err != nil {
		return err
	}
	for _, child := range children {
		if err := s.markSubtreeDeleted(ctx, ownerID, child.ID, deletedAt); err != nil {
			return err
		}
	}
	return nil
}

// Restore takes item — and, if it's a folder, everything under it — out of
// the trash. If its original parent is gone (deleted itself, or still in
// the trash), it's restored to the user's root instead. Descendants keep
// pointing at their existing parent within the subtree; only the top-level
// item's name/parent can change, on a collision at the destination.
func (s *ItemService) Restore(ctx context.Context, ownerID, id string) (*model.Item, error) {
	item, err := s.items.GetByID(ctx, id)
	if err != nil {
		if err == repository.ErrNotFound {
			return nil, apperr.NotFound
		}
		return nil, err
	}
	if item.OwnerID != ownerID || item.DeletedAt == nil {
		return nil, apperr.NotFound
	}

	username, err := s.username(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	trashPath := s.storage.TrashPath(username, item.ID, item.Name)

	restoreParentID := item.ParentID
	if restoreParentID != nil {
		parent, err := s.items.GetByID(ctx, *restoreParentID)
		if err != nil && err != repository.ErrNotFound {
			return nil, err
		}
		if err == repository.ErrNotFound || parent.DeletedAt != nil {
			restoreParentID = nil // original parent gone or still trashed — restore to root
		}
	}

	destFolderPath, err := s.folderPath(ctx, ownerID, username, restoreParentID)
	if err != nil {
		return nil, err
	}
	finalName, err := s.uniqueName(ctx, ownerID, restoreParentID, item.Type, item.Name, item.ID)
	if err != nil {
		return nil, err
	}
	destPath := filepath.Join(destFolderPath, finalName)

	if err := s.storage.Rename(trashPath, destPath); err != nil {
		return nil, err
	}

	now := s.now().Unix()
	if err := s.items.Restore(ctx, id, finalName, restoreParentID, now); err != nil {
		_ = s.storage.Rename(destPath, trashPath) // best-effort rollback
		return nil, err
	}
	if err := s.restoreDescendants(ctx, ownerID, item.ID, now); err != nil {
		return nil, err
	}

	item.DeletedAt = nil
	item.ParentID = restoreParentID
	item.Name = finalName
	item.UpdatedAt = now
	return item, nil
}

func (s *ItemService) restoreDescendants(ctx context.Context, ownerID, id string, updatedAt int64) error {
	children, err := s.items.ListChildrenDeleted(ctx, ownerID, &id)
	if err != nil {
		return err
	}
	for _, child := range children {
		if err := s.items.ClearDeletedAt(ctx, child.ID, updatedAt); err != nil {
			return err
		}
		if err := s.restoreDescendants(ctx, ownerID, child.ID, updatedAt); err != nil {
			return err
		}
	}
	return nil
}

// PermanentlyDelete removes a trashed item — and, if it's a folder,
// everything under it — for good: bytes off disk, rows out of the database.
func (s *ItemService) PermanentlyDelete(ctx context.Context, ownerID, id string) error {
	item, err := s.items.GetByID(ctx, id)
	if err != nil {
		if err == repository.ErrNotFound {
			return apperr.NotFound
		}
		return err
	}
	if item.OwnerID != ownerID || item.DeletedAt == nil {
		return apperr.NotFound
	}

	username, err := s.username(ctx, ownerID)
	if err != nil {
		return err
	}
	trashPath := s.storage.TrashPath(username, item.ID, item.Name)
	if err := s.storage.Remove(trashPath); err != nil {
		return err
	}
	return s.hardDeleteSubtreeRows(ctx, ownerID, item.ID)
}

func (s *ItemService) hardDeleteSubtreeRows(ctx context.Context, ownerID, id string) error {
	children, err := s.items.ListChildrenDeleted(ctx, ownerID, &id)
	if err != nil {
		return err
	}
	for _, child := range children {
		if err := s.hardDeleteSubtreeRows(ctx, ownerID, child.ID); err != nil {
			return err
		}
	}
	return s.items.HardDelete(ctx, id)
}
