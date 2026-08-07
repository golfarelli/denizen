package service

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	grants  *repository.UserShareRepository // direct per-user file shares — see GetIncludingTrashed
	storage *storage.Store
	now     func() time.Time // swappable in tests; defaults to time.Now
}

func NewItemService(items *repository.ItemRepository, users *repository.UserRepository, grants *repository.UserShareRepository, store *storage.Store) *ItemService {
	return &ItemService{items: items, users: users, grants: grants, storage: store, now: time.Now}
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

// GetIncludingTrashed is Get without the not-trashed requirement — for
// read-only access to an item regardless of trash state (previewing a
// trashed file before deciding whether to restore or delete it forever,
// matching Drive/Nextcloud's own trash behavior). Deliberately not just
// Get with a flag: every mutating operation (Move, Copy, ReplaceContent,
// ...) calls Get directly for its own precondition check, and those must
// keep rejecting a trashed item — only the read-only item/content/
// content-token handlers (internal/handler/item.go) use this one.
//
// Also the entry point for a direct per-user share's recipient (see
// model.UserShare): if callerID isn't the owner, a grant on this exact
// item is the only other way in — never into someone else's trash (a
// grant recipient gets no restore/trash-preview capability, unlike the
// owner, so a trashed target simply doesn't exist for them), and never
// through a folder (grants are files-only for now — see model.UserShare's
// own comment on why extending this to folders is a separate, bigger
// piece of work: real navigation/breadcrumb support for browsing into
// someone else's folder tree, not just a check removed here).
func (s *ItemService) GetIncludingTrashed(ctx context.Context, callerID, id string) (*model.Item, error) {
	item, err := s.items.GetByID(ctx, id)
	if err != nil {
		if err == repository.ErrNotFound {
			return nil, apperr.NotFound
		}
		return nil, err
	}
	if item.OwnerID == callerID {
		return item, nil
	}
	if item.DeletedAt != nil {
		return nil, apperr.NotFound
	}
	granted, err := s.grants.Exists(ctx, item.ID, callerID)
	if err != nil {
		return nil, err
	}
	if !granted {
		return nil, apperr.NotFound
	}
	return item, nil
}

// GetForShare returns an active item by ID with no ownership check — used
// only by the public share-resolution path (internal/service/share.go),
// where authorization comes from presenting a valid share token instead of
// being the item's owner. Regular authenticated access must go through Get.
func (s *ItemService) GetForShare(ctx context.Context, id string) (*model.Item, error) {
	item, err := s.items.GetByID(ctx, id)
	if err != nil {
		if err == repository.ErrNotFound {
			return nil, apperr.NotFound
		}
		return nil, err
	}
	if item.DeletedAt != nil {
		return nil, apperr.NotFound
	}
	return item, nil
}

// FilePath resolves the real on-disk path of an already-authorized file
// item. Callers (the private download/preview handlers, having called Get
// or GetIncludingTrashed; the public share handler, having validated a
// share token) are responsible for their own authorization before calling
// this — it doesn't re-check anything itself.
//
// A trashed item needs its own branch here: Delete (below) physically
// renames the *top-level* trashed item straight to storage.TrashPath, not
// the nested location pathOf would still compute from parent_id (which
// doesn't change on trash — only on Restore). This is only correct for
// that top-level entry, not an arbitrary descendant several folders deep
// inside a trashed subtree — but that's the only case any real caller
// hits: ListTrash (see its own comment) only ever surfaces one row per
// trashed subtree, its top-level item, which is the only thing Trash's
// own "open to view" (routes/trash/+page.svelte) can pass an id for in
// the first place.
func (s *ItemService) FilePath(ctx context.Context, item *model.Item) (string, error) {
	if item.Type != model.ItemTypeFile {
		return "", apperr.Validation("not a file")
	}
	username, err := s.username(ctx, item.OwnerID)
	if err != nil {
		return "", err
	}
	if item.DeletedAt != nil {
		return s.storage.TrashPath(username, item.ID, item.Name), nil
	}
	return s.pathOf(ctx, item, username)
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

// ValidateFolder checks that parentID (nil = the user's root) is a real,
// owned, active folder — used by the upload package's tus pre-create hook
// to fail fast, before any bytes are staged, instead of only discovering a
// bad target once the upload finishes (see internal/upload).
func (s *ItemService) ValidateFolder(ctx context.Context, ownerID string, parentID *string) error {
	if parentID == nil {
		return nil
	}
	item, err := s.Get(ctx, ownerID, *parentID)
	if err != nil {
		return err
	}
	if item.Type != model.ItemTypeFolder {
		return apperr.Validation("parent is not a folder")
	}
	return nil
}

// CheckQuota reports whether ownerID has enough quota left for
// additionalBytes more, plus an independent real-disk-space safety net
// (see internal/storage.FreeBytes) — quotas assigned to different users can
// add up to more than the disk actually has, so the logical check alone
// isn't enough. Side-effect-free: called both optimistically (the upload
// package's pre-create hook, against the client's declared upload size) and
// definitively (FinalizeUpload, right before committing the file) — see
// docs/ARCHITECTURE.md.
func (s *ItemService) CheckQuota(ctx context.Context, ownerID string, additionalBytes int64) error {
	user, err := s.users.GetByID(ctx, ownerID)
	if err != nil {
		return err
	}
	if user.StorageUsedBytes+additionalBytes > user.QuotaBytes {
		return apperr.QuotaExceeded
	}
	free, err := s.storage.FreeBytes()
	if err != nil {
		return err
	}
	if additionalBytes > int64(free) {
		return apperr.DiskFull
	}
	return nil
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

// FinalizeUpload registers a completed upload as a file item: it moves the
// finished upload from sourcePath (wherever the tus store staged it — see
// internal/upload) into the owner's real folder tree, computes its
// checksum at rest, creates its item row, and adjusts the owner's
// storage_used_bytes counter.
//
// sourcePath must be on the same filesystem as the destination (both live
// under the same data directory in the default layout — see
// docs/ARCHITECTURE.md), since the move is a Rename, not a copy.
//
// Note: the item row and the quota counter update below are two sequential
// statements, not one transaction — consistent with the rest of this
// codebase (e.g. Register creating a user then marking its invite used).
// A crash between them would leave the counter briefly behind the real
// total; acceptable for now, worth revisiting if/when the repository layer
// grows real transaction support.
func (s *ItemService) FinalizeUpload(ctx context.Context, ownerID string, parentID *string, name, sourcePath string, sizeBytes int64, mimeType string) (*model.Item, error) {
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
	finalName, err := s.uniqueName(ctx, ownerID, parentID, model.ItemTypeFile, name, "")
	if err != nil {
		return nil, err
	}

	// The definitive quota check (see CheckQuota) — right before touching
	// the real files tree, so a rejection here never leaves a partial file
	// where the user would see it.
	if err := s.CheckQuota(ctx, ownerID, sizeBytes); err != nil {
		return nil, err
	}

	targetPath := filepath.Join(folderPath, finalName)
	if err := s.storage.Rename(sourcePath, targetPath); err != nil {
		return nil, err
	}

	checksum, err := s.storage.Checksum(targetPath)
	if err != nil {
		_ = s.storage.Rename(targetPath, sourcePath) // best-effort: keep DB and disk in agreement
		return nil, err
	}

	now := s.now().Unix()
	item := &model.Item{
		ID:        idgen.New(),
		OwnerID:   ownerID,
		ParentID:  parentID,
		Name:      finalName,
		Type:      model.ItemTypeFile,
		SizeBytes: sizeBytes,
		Checksum:  &checksum,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if mimeType != "" {
		item.MimeType = &mimeType
	}
	if err := s.items.Create(ctx, item); err != nil {
		_ = s.storage.Rename(targetPath, sourcePath)
		return nil, err
	}

	if err := s.users.IncrementStorageUsed(ctx, ownerID, sizeBytes); err != nil {
		return nil, err
	}
	return item, nil
}

// ReplaceContent overwrites an existing file item's bytes in place —
// name, location, and id are untouched, only content (size, checksum,
// updated_at) and the owner's storage_used_bytes counter move. Used by the
// OnlyOffice save callback (internal/handler/onlyoffice.go) once the
// Document Server reports a document is ready to save; r is the response
// body of a GET against the URL that callback provides.
func (s *ItemService) ReplaceContent(ctx context.Context, ownerID, id string, r io.Reader) (*model.Item, error) {
	item, err := s.Get(ctx, ownerID, id)
	if err != nil {
		return nil, err
	}
	if item.Type != model.ItemTypeFile {
		return nil, apperr.Validation("not a file")
	}

	username, err := s.username(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	targetPath, err := s.pathOf(ctx, item, username)
	if err != nil {
		return nil, err
	}

	// Staged first, same as FinalizeUpload above and for the same reason
	// (storage.Store.StagingRoot's own doc comment): the final move is then
	// a cheap, atomic rename rather than a copy, and a failed/truncated
	// download from the Document Server never touches the file that's live
	// on disk until the rename below actually happens.
	if err := s.storage.CreateFolder(s.storage.StagingRoot()); err != nil {
		return nil, err
	}
	stagePath := filepath.Join(s.storage.StagingRoot(), idgen.New())
	written, err := s.storage.WriteFile(stagePath, r)
	if err != nil {
		return nil, err
	}

	// The definitive quota check (see CheckQuota's own doc comment) against
	// the size *delta*, not the new total — a shrinking edit should never
	// be blocked by a quota that's already fully used.
	if err := s.CheckQuota(ctx, ownerID, written-item.SizeBytes); err != nil {
		_ = s.storage.Remove(stagePath)
		return nil, err
	}

	checksum, err := s.storage.Checksum(stagePath)
	if err != nil {
		_ = s.storage.Remove(stagePath)
		return nil, err
	}

	// os.Rename (storage.Store.Rename) replaces an existing destination
	// atomically on the same filesystem — no separate remove-then-move
	// window where targetPath briefly doesn't exist for anyone reading it
	// concurrently.
	if err := s.storage.Rename(stagePath, targetPath); err != nil {
		_ = s.storage.Remove(stagePath)
		return nil, err
	}

	now := s.now().Unix()
	if err := s.items.UpdateContent(ctx, id, written, checksum, now); err != nil {
		return nil, err
	}
	if err := s.users.IncrementStorageUsed(ctx, ownerID, written-item.SizeBytes); err != nil {
		return nil, err
	}

	item.SizeBytes = written
	item.Checksum = &checksum
	item.UpdatedAt = now
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

// --- copy ---------------------------------------------------------------

// Copy duplicates item — and, if it's a folder, its whole active subtree —
// into destParentID (nil = root; the caller passes the item's own current
// parent explicitly for a same-folder "make a copy", the same way every
// other endpoint treats nil as root, not as an item-specific shorthand).
//
// A file copy is real new bytes on disk (never a hard link — see
// storage.CopyFile), so it's real new storage, checked against quota the
// same way an upload is (CheckQuota), once per file as it's copied rather
// than as one upfront total for a whole folder. A large folder can
// therefore end up partially copied if quota runs out partway through — a
// narrow edge case, flagged here rather than silently mishandled, in the
// same spirit as Delete's documented gap around already-individually-
// trashed descendants.
func (s *ItemService) Copy(ctx context.Context, ownerID, id string, destParentID *string) (*model.Item, error) {
	item, err := s.Get(ctx, ownerID, id)
	if err != nil {
		return nil, err
	}
	username, err := s.username(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	destFolderPath, err := s.folderPath(ctx, ownerID, username, destParentID)
	if err != nil {
		return nil, err
	}
	if item.Type == model.ItemTypeFolder && destParentID != nil {
		if err := s.checkNotSelfOrDescendant(ctx, item.ID, *destParentID); err != nil {
			return nil, err
		}
	}
	return s.copyRecursive(ctx, ownerID, username, item, destParentID, destFolderPath)
}

func (s *ItemService) copyRecursive(ctx context.Context, ownerID, username string, item *model.Item, destParentID *string, destFolderPath string) (*model.Item, error) {
	finalName, err := s.uniqueName(ctx, ownerID, destParentID, item.Type, item.Name, "")
	if err != nil {
		return nil, err
	}
	targetPath := filepath.Join(destFolderPath, finalName)
	now := s.now().Unix()

	if item.Type == model.ItemTypeFolder {
		if err := s.storage.CreateFolder(targetPath); err != nil {
			return nil, err
		}
		newItem := &model.Item{
			ID: idgen.New(), OwnerID: ownerID, ParentID: destParentID, Name: finalName,
			Type: model.ItemTypeFolder, CreatedAt: now, UpdatedAt: now,
		}
		if err := s.items.Create(ctx, newItem); err != nil {
			_ = s.storage.Remove(targetPath)
			return nil, err
		}
		children, err := s.items.ListChildren(ctx, ownerID, &item.ID)
		if err != nil {
			return newItem, err // the folder itself copied fine; report the error rather than roll it back
		}
		for _, child := range children {
			if _, err := s.copyRecursive(ctx, ownerID, username, child, &newItem.ID, targetPath); err != nil {
				return newItem, err
			}
		}
		return newItem, nil
	}

	if err := s.CheckQuota(ctx, ownerID, item.SizeBytes); err != nil {
		return nil, err
	}
	sourcePath, err := s.pathOf(ctx, item, username)
	if err != nil {
		return nil, err
	}
	if err := s.storage.CopyFile(sourcePath, targetPath); err != nil {
		return nil, err
	}
	newItem := &model.Item{
		ID: idgen.New(), OwnerID: ownerID, ParentID: destParentID, Name: finalName,
		Type: model.ItemTypeFile, SizeBytes: item.SizeBytes, MimeType: item.MimeType, Checksum: item.Checksum,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.items.Create(ctx, newItem); err != nil {
		_ = s.storage.Remove(targetPath)
		return nil, err
	}
	if err := s.users.IncrementStorageUsed(ctx, ownerID, item.SizeBytes); err != nil {
		return nil, err
	}
	return newItem, nil
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

// PurgeExpiredTrash permanently deletes every trashed item, across every
// user, whose deleted_at is older than retention — the scheduled
// counterpart to the 30-day auto-purge policy from docs/ARCHITECTURE.md
// (cmd/server/main.go calls this on a timer; see config.TrashPurgeInterval/
// TrashRetention).
//
// Only top-level entries within the expired set are purged directly — the
// same "one trash entry per deletion, not one per nested file" collapsing
// ListTrash does — since PermanentlyDelete already recurses through a
// subtree's descendants on its own. A single item's failure doesn't stop
// the rest of the sweep; every error is collected and returned together.
func (s *ItemService) PurgeExpiredTrash(ctx context.Context, retention time.Duration) (purged int, err error) {
	cutoff := s.now().Add(-retention).Unix()
	expired, err := s.items.ListTrashedBefore(ctx, cutoff)
	if err != nil {
		return 0, err
	}

	expiredIDs := make(map[string]bool, len(expired))
	for _, item := range expired {
		expiredIDs[item.ID] = true
	}

	var errs []error
	for _, item := range expired {
		if item.ParentID != nil && expiredIDs[*item.ParentID] {
			continue // not top-level within this batch — its ancestor's purge recurses through it
		}
		if err := s.PermanentlyDelete(ctx, item.OwnerID, item.ID); err != nil {
			errs = append(errs, fmt.Errorf("purge item %s: %w", item.ID, err))
			continue
		}
		purged++
	}
	return purged, errors.Join(errs...)
}

// hardDeleteSubtreeRows removes id's row (and, recursively, its
// descendants') for good, freeing each file's bytes from the owner's quota
// counter as it goes — this is the only path that both destroys a file and
// permanently gives its space back (soft delete keeps counting it, exactly
// as decided for the trash/quota interaction — see docs/ARCHITECTURE.md).
func (s *ItemService) hardDeleteSubtreeRows(ctx context.Context, ownerID, id string) error {
	item, err := s.items.GetByID(ctx, id)
	if err != nil {
		return err
	}
	children, err := s.items.ListChildrenDeleted(ctx, ownerID, &id)
	if err != nil {
		return err
	}
	for _, child := range children {
		if err := s.hardDeleteSubtreeRows(ctx, ownerID, child.ID); err != nil {
			return err
		}
	}
	if err := s.items.HardDelete(ctx, id); err != nil {
		return err
	}
	if item.Type == model.ItemTypeFile {
		return s.users.IncrementStorageUsed(ctx, ownerID, -item.SizeBytes)
	}
	return nil
}
