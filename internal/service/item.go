package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/golfarelli/denizen/internal/apperr"
	"github.com/golfarelli/denizen/internal/idgen"
	"github.com/golfarelli/denizen/internal/model"
	"github.com/golfarelli/denizen/internal/repository"
	"github.com/golfarelli/denizen/internal/storage"
	"github.com/golfarelli/denizen/internal/textextract"
)

// ItemService covers folders and (once upload lands) files: creating,
// listing, moving/renaming, and trash (soft delete, restore, permanent
// delete). It's also the only layer that knows an item's ID maps to a real
// path on disk — see pathOf/folderPath below.
type ItemService struct {
	items        *repository.ItemRepository
	users        *repository.UserRepository
	grants       *repository.UserShareRepository // direct per-user file shares — see GetIncludingTrashed
	search       *repository.SearchRepository    // content search index — see indexContent/Search
	ocr          *repository.OCRRepository       // scanned-PDF/photo OCR attempt tracking — see RunOCRSweep
	ocrBatchSize int                             // see TriggerOCRSweep
	storage      *storage.Store
	now          func() time.Time // swappable in tests; defaults to time.Now

	// ocrSweeping guards RunOCRSweep against running two passes
	// concurrently — see TriggerOCRSweep, the only thing that flips it.
	ocrSweeping atomic.Bool
}

func NewItemService(items *repository.ItemRepository, users *repository.UserRepository, grants *repository.UserShareRepository, search *repository.SearchRepository, ocr *repository.OCRRepository, ocrBatchSize int, store *storage.Store) *ItemService {
	return &ItemService{items: items, users: users, grants: grants, search: search, ocr: ocr, ocrBatchSize: ocrBatchSize, storage: store, now: time.Now}
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
// Get with a flag: mutating operations need their own, edit-permission-
// aware precondition (see GetForWrite) and must keep rejecting a trashed
// item regardless of who's asking — only the read-only item/content/
// content-token handlers (internal/handler/item.go) use this one.
//
// Also the entry point for a direct per-user share's recipient (see
// model.UserShare): if callerID isn't the owner, a grant on this item or
// an ancestor folder of it (see resolveGrant) is the only other way in —
// at any permission level, view or edit — but never into someone else's
// trash (a grant recipient gets no restore/trash-preview capability,
// unlike the owner, so a trashed target simply doesn't exist for them).
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
	grant, err := s.resolveGrant(ctx, item, callerID)
	if err != nil {
		return nil, err
	}
	if grant == nil {
		return nil, apperr.NotFound
	}
	return item, nil
}

// ResolveContentItem is GetIncludingTrashed plus one extra step: if id
// turns out to be a shortcut, it re-resolves callerID's access against the
// *target* right now, rather than trusting that access implied at the
// shortcut's own creation time still holds. A share revoked since then
// correctly breaks the shortcut here (GetIncludingTrashed on the target
// returns apperr.NotFound) instead of silently still serving content the
// caller can no longer see any other way. Content/ContentToken (the only
// two handlers that ever serve bytes for an item) use this instead of
// GetIncludingTrashed directly; every other read (List, Get, Search, ...)
// deliberately keeps showing the shortcut's own row — snapshot name and
// all — without this extra round trip, see model.Item's own TargetID
// comment on why that's a fine simplification there.
func (s *ItemService) ResolveContentItem(ctx context.Context, callerID, id string) (*model.Item, error) {
	item, err := s.GetIncludingTrashed(ctx, callerID, id)
	if err != nil {
		return nil, err
	}
	if item.TargetID == nil {
		return item, nil
	}
	return s.GetIncludingTrashed(ctx, callerID, *item.TargetID)
}

// resolveGrant finds the user_shares grant, if any, that gives callerID
// access to item — either a direct grant on item itself, or on the
// nearest shared ancestor folder above it (a folder share is inherited by
// everything nested inside it, same as Drive). Returns (nil, nil), not an
// error, when there's simply no path in at all — every caller here treats
// that as apperr.NotFound (or, for CanEdit, "no").
func (s *ItemService) resolveGrant(ctx context.Context, item *model.Item, callerID string) (*model.UserShare, error) {
	current := item
	for {
		grant, err := s.grants.FindGrant(ctx, current.ID, callerID)
		if err != nil && err != repository.ErrNotFound {
			return nil, err
		}
		if grant != nil {
			return grant, nil
		}
		if current.ParentID == nil {
			return nil, nil
		}
		parent, err := s.items.GetByID(ctx, *current.ParentID)
		if err != nil {
			if err == repository.ErrNotFound {
				return nil, nil
			}
			return nil, err
		}
		current = parent
	}
}

// CanEdit reports whether callerID may modify item's content or, for a
// folder, create/upload/rename/move/delete within it: true for the owner,
// or a direct/inherited share grant (resolveGrant) at 'edit' permission.
// Call after read access to item has already been established (Get/
// GetIncludingTrashed) — this doesn't re-check that on its own.
func (s *ItemService) CanEdit(ctx context.Context, callerID string, item *model.Item) (bool, error) {
	if item.OwnerID == callerID {
		return true, nil
	}
	grant, err := s.resolveGrant(ctx, item, callerID)
	if err != nil {
		return false, err
	}
	return grant != nil && grant.Permission == model.SharePermissionEdit, nil
}

// SharedUsernames returns, for each of itemIDs, the usernames it's been
// directly shared with — the file browser's "who has access" row badge.
// Callers are expected to only ever pass ids the caller actually owns
// (List/ListChildren already scope what a caller can see); this does no
// ownership check of its own, same trust boundary as the repository call
// underneath it.
func (s *ItemService) SharedUsernames(ctx context.Context, itemIDs []string) (map[string][]string, error) {
	grants, err := s.grants.ListByItems(ctx, itemIDs)
	if err != nil {
		return nil, err
	}
	usernames := make(map[string]string, len(grants))
	out := make(map[string][]string, len(grants))
	for _, grant := range grants {
		username, ok := usernames[grant.SharedWithID]
		if !ok {
			username = "?"
			if u, err := s.users.GetByID(ctx, grant.SharedWithID); err == nil {
				username = u.Username
			}
			usernames[grant.SharedWithID] = username
		}
		out[grant.ItemID] = append(out[grant.ItemID], username)
	}
	return out, nil
}

// GetForWrite is the write-side counterpart to GetIncludingTrashed: the
// owner, or someone holding 'edit' access to item (directly, or inherited
// from a shared ancestor folder — see resolveGrant), and never a trashed
// item, for the owner either (matching Get, not GetIncludingTrashed's own
// trash-preview carve-out). Every mutating ItemService method that a
// share recipient can now reach (Move, Delete, Copy's destination,
// CreateFolder, FinalizeUpload, ReplaceContent) goes through this instead
// of Get.
//
// Two different failure shapes on purpose, same distinction Get already
// makes for ownership: apperr.NotFound when callerID has no relationship
// to item at all (never reveals it exists, same as an unrelated stranger
// hitting Get on someone else's item), apperr.Forbidden only once a grant
// already proves callerID can at least see it, just not edit it.
func (s *ItemService) GetForWrite(ctx context.Context, callerID, id string) (*model.Item, error) {
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
	if item.OwnerID == callerID {
		return item, nil
	}
	grant, err := s.resolveGrant(ctx, item, callerID)
	if err != nil {
		return nil, err
	}
	if grant == nil {
		return nil, apperr.NotFound
	}
	if grant.Permission != model.SharePermissionEdit {
		return nil, apperr.Forbidden
	}
	return item, nil
}

// getReadable is Copy's own precondition: the owner, or a grant (any
// permission — view is enough to "make a copy", same as Drive) on this
// item or an ancestor of it, and never a trashed item, even the owner's
// own (Copy never supported that, no reason to start now — matches Get,
// not GetIncludingTrashed's trash-preview carve-out).
func (s *ItemService) getReadable(ctx context.Context, callerID, id string) (*model.Item, error) {
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
	if item.OwnerID == callerID {
		return item, nil
	}
	grant, err := s.resolveGrant(ctx, item, callerID)
	if err != nil {
		return nil, err
	}
	if grant == nil {
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
//
// callerID is who's asking, not necessarily whose tree gets listed:
// browsing into a folder shared with callerID (directly, or inherited
// from a shared ancestor — see resolveGrant) lists that folder owner's
// children instead of callerID's own, at any grant permission (view is
// enough to browse in). canEdit reports whether callerID may also
// create/upload/rename/move/delete within parentID — always true for the
// caller's own root (parentID == nil), meaningless to a caller that
// ignores it otherwise.
func (s *ItemService) ListChildren(ctx context.Context, callerID string, parentID *string) (items []*model.Item, canEdit bool, err error) {
	ownerID := callerID
	canEdit = true
	if parentID != nil {
		parent, err := s.GetIncludingTrashed(ctx, callerID, *parentID)
		if err != nil {
			return nil, false, err
		}
		if parent.Type != model.ItemTypeFolder {
			return nil, false, apperr.Validation("not a folder")
		}
		if parent.DeletedAt != nil {
			return nil, false, apperr.NotFound // trash has its own ListTrash/Restore flow, not this one
		}
		ownerID = parent.OwnerID
		if canEdit, err = s.CanEdit(ctx, callerID, parent); err != nil {
			return nil, false, err
		}
	}
	items, err = s.items.ListChildren(ctx, ownerID, parentID)
	return items, canEdit, err
}

// searchResultLimit caps how many rows Search ever returns — plenty for a
// personal drive's "did I name it X" / "what mentions Y" lookup, and a
// hard ceiling on how much a single query makes the FTS5/LIKE scan (and
// the client rendering the results) do.
const searchResultLimit = 50

// Search finds every active item ownerID may read — their own, or
// reached through a direct/inherited share grant, same reach as
// GetIncludingTrashed — whose name or indexed content matches query.
// GET /api/v1/search's own logic. Own name matches rank first (someone
// searching almost always remembers roughly what they called a file),
// then shared-subtree name matches, then content matches by FTS5
// relevance; each item appears once even if it matched more than one way.
func (s *ItemService) Search(ctx context.Context, ownerID, query string) ([]*model.Item, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	seen := make(map[string]bool)
	var results []*model.Item
	add := func(item *model.Item) {
		if seen[item.ID] || len(results) >= searchResultLimit {
			return
		}
		seen[item.ID] = true
		results = append(results, item)
	}

	ownMatches, err := s.items.SearchByName(ctx, ownerID, query, searchResultLimit)
	if err != nil {
		return nil, err
	}
	for _, item := range ownMatches {
		add(item)
	}

	// A grant on a folder is inherited by everything nested inside it,
	// same as browsing (resolveGrant) — SearchByNameInSubtree is the
	// name-search equivalent of that same walk, downward from each
	// directly-shared root instead of upward from one target item.
	if len(results) < searchResultLimit {
		grants, err := s.grants.ListReceivedBy(ctx, ownerID)
		if err != nil {
			return nil, err
		}
		for _, grant := range grants {
			if len(results) >= searchResultLimit {
				break
			}
			subtreeMatches, err := s.items.SearchByNameInSubtree(ctx, grant.ItemID, query, searchResultLimit)
			if err != nil {
				return nil, err
			}
			for _, item := range subtreeMatches {
				add(item)
			}
		}
	}

	if len(results) < searchResultLimit {
		contentIDs, err := s.search.SearchContent(ctx, query, searchResultLimit)
		if err != nil {
			return nil, err
		}
		var unresolved []string
		for _, id := range contentIDs {
			if !seen[id] {
				unresolved = append(unresolved, id)
			}
		}
		if len(unresolved) > 0 {
			contentItems, err := s.items.GetByIDs(ctx, unresolved)
			if err != nil {
				return nil, err
			}
			byID := make(map[string]*model.Item, len(contentItems))
			for _, item := range contentItems {
				byID[item.ID] = item
			}
			// Re-apply SearchContent's own rank order (GetByIDs doesn't
			// preserve it) and check read access the same way any other
			// item read does — a standalone FTS5 index enforces neither
			// ownership nor trash state on its own (see SearchRepository's
			// own doc comment).
			for _, id := range unresolved {
				if len(results) >= searchResultLimit {
					break
				}
				item, ok := byID[id]
				if !ok || item.DeletedAt != nil {
					continue
				}
				if item.OwnerID == ownerID {
					add(item)
					continue
				}
				grant, err := s.resolveGrant(ctx, item, ownerID)
				if err != nil {
					return nil, err
				}
				if grant != nil {
					add(item)
				}
			}
		}
	}

	return results, nil
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

// ValidateFolder checks that parentID (nil = callerID's own root) is a
// real, active folder callerID may create things in — their own, or one
// they hold 'edit' access to via a direct/inherited share grant (see
// GetForWrite) — and returns the id of whoever's storage tree it actually
// lives under (== callerID unless it's a shared folder). Used by the
// upload package's tus pre-create hook to fail fast, before any bytes are
// staged, instead of only discovering a bad target once the upload
// finishes (see internal/upload), and reused by CreateFolder/
// FinalizeUpload themselves for the exact same resolution.
func (s *ItemService) ValidateFolder(ctx context.Context, callerID string, parentID *string) (string, error) {
	if parentID == nil {
		return callerID, nil
	}
	item, err := s.GetForWrite(ctx, callerID, *parentID)
	if err != nil {
		return "", err
	}
	if item.Type != model.ItemTypeFolder {
		return "", apperr.Validation("parent is not a folder")
	}
	return item.OwnerID, nil
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

// CreateFolder creates a new folder under parentID (nil = callerID's own
// root). Files aren't created through this — they arrive via the upload
// endpoint instead, since a file needs bytes, not just a name.
//
// parentID may be a folder shared with callerID at 'edit' permission
// (ValidateFolder resolves and authorizes that) — the new folder is then
// owned by whoever's shared folder it was created in, not by callerID.
func (s *ItemService) CreateFolder(ctx context.Context, callerID string, parentID *string, name string) (*model.Item, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)

	ownerID, err := s.ValidateFolder(ctx, callerID, parentID)
	if err != nil {
		return nil, err
	}

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

// CreateShortcut adds a pointer to targetID inside destParentID (nil = the
// caller's own root) — no storage.* calls at all, unlike CreateFolder just
// above: a shortcut has no counterpart on disk (see model.Item's own
// TargetID comment). callerID needs read access to the target (owns it,
// or reached it via a share — getReadable, the same check Copy uses) and
// write access to the destination (ValidateFolder, same as CreateFolder/
// Move — and the same "owned by whoever the destination folder belongs
// to, not necessarily the caller" rule that already applies there).
//
// A shortcut to a shortcut flattens to the real target instead of
// chaining, so every write path below (Move/Delete/Restore/
// PermanentlyDelete/Copy) only ever has to consider one level of
// indirection.
//
// Name and MimeType are a snapshot of the target taken right now, not
// resolved live on every future read — ponytail: renaming the real target
// later won't update this shortcut's own displayed name; add a live JOIN
// at read time (List/Get/Search) if that drifts enough in practice to
// bother Fabio, not worth the extra query on every listing for a
// personal-scale drive today.
func (s *ItemService) CreateShortcut(ctx context.Context, callerID, targetID string, destParentID *string) (*model.Item, error) {
	target, err := s.getReadable(ctx, callerID, targetID)
	if err != nil {
		return nil, err
	}
	if target.TargetID != nil {
		target, err = s.getReadable(ctx, callerID, *target.TargetID)
		if err != nil {
			return nil, err
		}
	}

	destOwnerID, err := s.ValidateFolder(ctx, callerID, destParentID)
	if err != nil {
		return nil, err
	}

	finalName, err := s.uniqueName(ctx, destOwnerID, destParentID, target.Type, target.Name, "")
	if err != nil {
		return nil, err
	}

	now := s.now().Unix()
	item := &model.Item{
		ID:        idgen.New(),
		OwnerID:   destOwnerID,
		ParentID:  destParentID,
		Name:      finalName,
		Type:      target.Type,
		MimeType:  target.MimeType,
		TargetID:  &target.ID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.items.Create(ctx, item); err != nil {
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
// callerID is who's uploading, not necessarily who owns the result:
// parentID may be a folder shared with callerID at 'edit' permission (see
// ValidateFolder), in which case the new file — and the quota it counts
// against — belongs to that folder's real owner instead.
func (s *ItemService) FinalizeUpload(ctx context.Context, callerID string, parentID *string, name, sourcePath string, sizeBytes int64, mimeType string) (*model.Item, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)

	ownerID, err := s.ValidateFolder(ctx, callerID, parentID)
	if err != nil {
		return nil, err
	}

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
	s.indexContent(ctx, item.ID, item.Name, targetPath)
	s.TriggerOCRSweep(s.ocrBatchSize)
	return item, nil
}

// ReplaceContent overwrites an existing file item's bytes in place —
// name, location, and id are untouched, only content (size, checksum,
// updated_at) and the owner's storage_used_bytes counter move. Used by the
// OnlyOffice save callback (internal/handler/onlyoffice.go) once the
// Document Server reports a document is ready to save; r is the response
// body of a GET against the URL that callback provides.
// callerID needs only 'edit' access (GetForWrite), not ownership — see
// model.UserShare — but every disk/quota effect below still lands on
// item.OwnerID, the real owner, never callerID.
func (s *ItemService) ReplaceContent(ctx context.Context, callerID, id string, r io.Reader) (*model.Item, error) {
	item, err := s.GetForWrite(ctx, callerID, id)
	if err != nil {
		return nil, err
	}
	if item.Type != model.ItemTypeFile {
		return nil, apperr.Validation("not a file")
	}

	username, err := s.username(ctx, item.OwnerID)
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
	if err := s.CheckQuota(ctx, item.OwnerID, written-item.SizeBytes); err != nil {
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
	if err := s.users.IncrementStorageUsed(ctx, item.OwnerID, written-item.SizeBytes); err != nil {
		return nil, err
	}
	s.indexContent(ctx, item.ID, item.Name, targetPath)
	// A prior version of this file may have been a blank scan the OCR
	// sweep already gave up on (see RunOCRSweep) — the new bytes deserve
	// their own fresh attempt instead of staying permanently skipped over
	// something that no longer even exists. Best-effort, same as the
	// index update just above: a stale marker left behind by a failure
	// here just costs one skipped OCR attempt, never a broken upload.
	if err := s.ocr.ClearAttempt(ctx, item.ID); err != nil {
		log.Printf("ocr: clear attempt for %s: %v", item.ID, err)
	}
	s.TriggerOCRSweep(s.ocrBatchSize)

	item.SizeBytes = written
	item.Checksum = &checksum
	item.UpdatedAt = now
	return item, nil
}

// RunOCRSweep looks for up to batchSize scanned PDFs or photographed
// documents with no indexed text yet (OCRRepository.ListPending) and runs
// OCR against each in turn — pdftoppm+tesseract for a PDF (ExtractOCRPDF,
// one rasterize-then-recognize pass per page), tesseract directly for a
// photo (ExtractOCRImage, no separate fast path even exists for those).
// pdftotext's synchronous fast path (indexContent, called from
// FinalizeUpload/ReplaceContent) already handles every PDF with a real
// text layer instantly; this only ever picks up what came back empty
// (PDFs) or was never attempted at all (photos), and runs later — from a
// periodic background sweep, see cmd/server/main.go — instead of blocking
// the upload that created them, since real OCR (actual image recognition)
// is far slower than pdftotext. Every candidate gets marked attempted
// whether or not OCR found anything, so a genuinely blank/corrupt file
// only ever costs CPU once. Returns how many candidates it looked at.
func (s *ItemService) RunOCRSweep(ctx context.Context, batchSize int) (int, error) {
	candidates, err := s.ocr.ListPending(ctx, batchSize)
	if err != nil {
		return 0, err
	}
	usernames := make(map[string]string)
	for _, item := range candidates {
		username, ok := usernames[item.OwnerID]
		if !ok {
			username, err = s.username(ctx, item.OwnerID)
			if err != nil {
				return 0, err
			}
			usernames[item.OwnerID] = username
		}
		path, err := s.pathOf(ctx, item, username)
		var text string
		var extractErr error
		if err != nil {
			log.Printf("ocr sweep: path for %s (%s): %v", item.ID, item.Name, err)
		} else if textextract.ExtFor(item.Name) == "pdf" {
			text, extractErr = textextract.ExtractOCRPDF(path)
		} else {
			text, extractErr = textextract.ExtractOCRImage(path)
		}
		if extractErr != nil {
			log.Printf("ocr sweep: extract %s (%s): %v", item.ID, item.Name, extractErr)
		} else if text != "" {
			if err := s.search.IndexContent(ctx, item.ID, text); err != nil {
				log.Printf("ocr sweep: store %s: %v", item.ID, err)
			}
		}
		if err := s.ocr.MarkAttempted(ctx, item.ID, s.now().Unix()); err != nil {
			log.Printf("ocr sweep: mark attempted %s: %v", item.ID, err)
		}
	}
	return len(candidates), nil
}

// TriggerOCRSweep kicks a background RunOCRSweep pass in its own
// goroutine, unless one is already running — from a previous trigger or
// the periodic tick in cmd/server/main.go, both of which call this same
// method, sharing one guard — in which case this is a no-op: ListPending
// would just find whatever's already queued anyway, so a redundant pass
// only wastes CPU without finding anything new. Called right after every
// upload/replace (FinalizeUpload/ReplaceContent) so a scanned PDF or
// photographed document becomes searchable within seconds instead of
// waiting for the next scheduled tick, which can be minutes away.
func (s *ItemService) TriggerOCRSweep(batchSize int) {
	if !s.ocrSweeping.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer s.ocrSweeping.Store(false)
		attempted, err := s.RunOCRSweep(context.Background(), batchSize)
		if err != nil {
			log.Printf("ocr sweep: %v", err)
		} else if attempted > 0 {
			log.Printf("ocr sweep: attempted %d file(s)", attempted)
		}
	}()
}

// ReindexAllContent walks every active file and (re-)builds its content
// search index — a one-off backfill for files uploaded before content
// search existed, which indexContent's normal call sites (FinalizeUpload,
// ReplaceContent) never touch since neither fires again for a file that's
// just sitting there. Not wired to any HTTP route or scheduled sweep on
// purpose — this is meant to be run once via the server's -reindex-content
// flag, not something a user or a timer triggers. Usernames are cached
// across items since most files share an owner. Returns how many files it
// attempted, an extraction/index failure for one file (already logged by
// indexContent) doesn't stop the rest.
func (s *ItemService) ReindexAllContent(ctx context.Context) (int, error) {
	files, err := s.items.ListAllFiles(ctx)
	if err != nil {
		return 0, err
	}
	usernames := make(map[string]string)
	for _, item := range files {
		username, ok := usernames[item.OwnerID]
		if !ok {
			username, err = s.username(ctx, item.OwnerID)
			if err != nil {
				return 0, err
			}
			usernames[item.OwnerID] = username
		}
		path, err := s.pathOf(ctx, item, username)
		if err != nil {
			log.Printf("reindex: path for %s (%s): %v", item.ID, item.Name, err)
			continue
		}
		s.indexContent(ctx, item.ID, item.Name, path)
	}
	return len(files), nil
}

// indexContent (re-)builds the search index for a file that was just
// uploaded or edited (FinalizeUpload/ReplaceContent above, after the item
// row itself has already been committed) — best-effort: extraction
// failing (unsupported type, corrupt file, pdftotext missing, ...) never
// fails the upload/edit itself, that file just won't turn up in a content
// search. See internal/textextract's own doc comment.
func (s *ItemService) indexContent(ctx context.Context, itemID, name, path string) {
	ext := textextract.ExtFor(name)
	if !textextract.Supported(ext) {
		return
	}
	text, err := textextract.Extract(path, ext)
	if err != nil {
		log.Printf("search index: extract %s (%s): %v", itemID, ext, err)
		return
	}
	if err := s.search.IndexContent(ctx, itemID, text); err != nil {
		log.Printf("search index: store %s: %v", itemID, err)
	}
}

// --- move / rename ----------------------------------------------------------

// MoveInput is a full replacement of an item's name and location — like the
// rest of this codebase's PATCH endpoints, both fields are always supplied
// by the caller, not merged in partially.
type MoveInput struct {
	Name     string
	ParentID *string // nil = the user's root
}

// Move renames and/or reparents an item, moving its bytes on disk to
// match. callerID needs only 'edit' access (GetForWrite), not ownership.
//
// in.ParentID == nil ("root") always means the root of item's own
// existing tree — item.OwnerID's root, not necessarily callerID's — since
// Move never changes who owns an item, only where it sits; see the
// destOwnerID != item.OwnerID check below for why a reparent can also
// never smuggle an item across into a different person's drive.
func (s *ItemService) Move(ctx context.Context, callerID, id string, in MoveInput) (*model.Item, error) {
	if err := validateName(in.Name); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.Name)

	item, err := s.GetForWrite(ctx, callerID, id)
	if err != nil {
		return nil, err
	}

	if in.ParentID != nil && item.Type == model.ItemTypeFolder && item.TargetID == nil {
		if err := s.checkNotSelfOrDescendant(ctx, item.ID, *in.ParentID); err != nil {
			return nil, err
		}
	}

	destOwnerID := item.OwnerID
	if in.ParentID != nil {
		if destOwnerID, err = s.ValidateFolder(ctx, callerID, in.ParentID); err != nil {
			return nil, err
		}
		if destOwnerID != item.OwnerID {
			return nil, apperr.Validation("cannot move an item into a different person's drive")
		}
	}

	finalName, err := s.uniqueName(ctx, item.OwnerID, in.ParentID, item.Type, name, item.ID)
	if err != nil {
		return nil, err
	}

	// A shortcut has no counterpart on disk to rename/move — only its own
	// row changes (see model.Item's own TargetID comment).
	var oldPath, newPath string
	if item.TargetID == nil {
		username, err := s.username(ctx, item.OwnerID)
		if err != nil {
			return nil, err
		}
		oldPath, err = s.pathOf(ctx, item, username)
		if err != nil {
			return nil, err
		}
		newFolderPath, err := s.folderPath(ctx, item.OwnerID, username, in.ParentID)
		if err != nil {
			return nil, err
		}
		newPath = filepath.Join(newFolderPath, finalName)
		if newPath != oldPath {
			if err := s.storage.Rename(oldPath, newPath); err != nil {
				return nil, err
			}
		}
	}

	now := s.now().Unix()
	if err := s.items.UpdateNameParent(ctx, id, finalName, in.ParentID, now); err != nil {
		if item.TargetID == nil && newPath != oldPath {
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
// Copy needs only read access to the source (getReadable: owner, or any
// grant — view is enough to "make a copy", like Drive) but 'edit' access
// to the destination (ValidateFolder) — the two can be different people's
// drives (copying something shared with callerID into their own root, or
// copying callerID's own file into a folder shared with them), so source
// and destination owners are resolved and threaded through separately.
func (s *ItemService) Copy(ctx context.Context, callerID, id string, destParentID *string) (*model.Item, error) {
	item, err := s.getReadable(ctx, callerID, id)
	if err != nil {
		return nil, err
	}

	// Copying a shortcut creates another shortcut to the same target,
	// same as Drive — there's no real content here for copyRecursive
	// below to copy. CreateShortcut always flattens at creation time (see
	// its own comment), so item.TargetID already points straight at the
	// real item, never at another shortcut.
	if item.TargetID != nil {
		return s.CreateShortcut(ctx, callerID, *item.TargetID, destParentID)
	}

	sourceUsername, err := s.username(ctx, item.OwnerID)
	if err != nil {
		return nil, err
	}

	destOwnerID, err := s.ValidateFolder(ctx, callerID, destParentID)
	if err != nil {
		return nil, err
	}
	destUsername, err := s.username(ctx, destOwnerID)
	if err != nil {
		return nil, err
	}
	destFolderPath, err := s.folderPath(ctx, destOwnerID, destUsername, destParentID)
	if err != nil {
		return nil, err
	}
	if item.Type == model.ItemTypeFolder && destParentID != nil {
		if err := s.checkNotSelfOrDescendant(ctx, item.ID, *destParentID); err != nil {
			return nil, err
		}
	}
	return s.copyRecursive(ctx, destOwnerID, sourceUsername, item, destParentID, destFolderPath)
}

// copyRecursive walks the source subtree (item, owned by whoever it was
// before Copy was ever called — every descendant fetched here shares that
// same owner, so sourceUsername never needs re-resolving as the recursion
// descends) and recreates it under destOwnerID.
func (s *ItemService) copyRecursive(ctx context.Context, destOwnerID, sourceUsername string, item *model.Item, destParentID *string, destFolderPath string) (*model.Item, error) {
	finalName, err := s.uniqueName(ctx, destOwnerID, destParentID, item.Type, item.Name, "")
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
			ID: idgen.New(), OwnerID: destOwnerID, ParentID: destParentID, Name: finalName,
			Type: model.ItemTypeFolder, CreatedAt: now, UpdatedAt: now,
		}
		if err := s.items.Create(ctx, newItem); err != nil {
			_ = s.storage.Remove(targetPath)
			return nil, err
		}
		children, err := s.items.ListChildren(ctx, item.OwnerID, &item.ID)
		if err != nil {
			return newItem, err // the folder itself copied fine; report the error rather than roll it back
		}
		for _, child := range children {
			if _, err := s.copyRecursive(ctx, destOwnerID, sourceUsername, child, &newItem.ID, targetPath); err != nil {
				return newItem, err
			}
		}
		return newItem, nil
	}

	if err := s.CheckQuota(ctx, destOwnerID, item.SizeBytes); err != nil {
		return nil, err
	}
	sourcePath, err := s.pathOf(ctx, item, sourceUsername)
	if err != nil {
		return nil, err
	}
	if err := s.storage.CopyFile(sourcePath, targetPath); err != nil {
		return nil, err
	}
	newItem := &model.Item{
		ID: idgen.New(), OwnerID: destOwnerID, ParentID: destParentID, Name: finalName,
		Type: model.ItemTypeFile, SizeBytes: item.SizeBytes, MimeType: item.MimeType, Checksum: item.Checksum,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.items.Create(ctx, newItem); err != nil {
		_ = s.storage.Remove(targetPath)
		return nil, err
	}
	if err := s.users.IncrementStorageUsed(ctx, destOwnerID, item.SizeBytes); err != nil {
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
// callerID needs only 'edit' access (GetForWrite), not ownership — trashes
// into item.OwnerID's own trash, same as if the owner had deleted it
// themselves (a grant recipient still gets no restore/trash-preview
// capability of their own — see GetIncludingTrashed).
func (s *ItemService) Delete(ctx context.Context, callerID, id string) error {
	item, err := s.GetForWrite(ctx, callerID, id)
	if err != nil {
		return err
	}

	// A shortcut has no counterpart on disk to move into trash — only its
	// own row goes (see model.Item's own TargetID comment); markSubtreeDeleted
	// is DB-only regardless, real physical trashing only ever happens here,
	// for the top-level item Delete was actually called on.
	if item.TargetID != nil {
		return s.markSubtreeDeleted(ctx, item.OwnerID, item.ID, s.now().Unix())
	}

	username, err := s.username(ctx, item.OwnerID)
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
	if err := s.markSubtreeDeleted(ctx, item.OwnerID, item.ID, now); err != nil {
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

	finalName, err := s.uniqueName(ctx, ownerID, restoreParentID, item.Type, item.Name, item.ID)
	if err != nil {
		return nil, err
	}

	// A shortcut never had a real trash path to begin with (Delete skips
	// storage for one — see model.Item's own TargetID comment), so there's
	// nothing to rename back.
	if item.TargetID == nil {
		trashPath := s.storage.TrashPath(username, item.ID, item.Name)
		destFolderPath, err := s.folderPath(ctx, ownerID, username, restoreParentID)
		if err != nil {
			return nil, err
		}
		destPath := filepath.Join(destFolderPath, finalName)
		if err := s.storage.Rename(trashPath, destPath); err != nil {
			return nil, err
		}
	}

	now := s.now().Unix()
	if err := s.items.Restore(ctx, id, finalName, restoreParentID, now); err != nil {
		if item.TargetID == nil {
			trashPath := s.storage.TrashPath(username, item.ID, item.Name)
			destFolderPath, _ := s.folderPath(ctx, ownerID, username, restoreParentID)
			_ = s.storage.Rename(filepath.Join(destFolderPath, finalName), trashPath) // best-effort rollback
		}
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

	// A shortcut never had a real trash path (Delete skips storage for
	// one — see model.Item's own TargetID comment), so there's nothing on
	// disk to remove here either.
	if item.TargetID == nil {
		username, err := s.username(ctx, ownerID)
		if err != nil {
			return err
		}
		trashPath := s.storage.TrashPath(username, item.ID, item.Name)
		if err := s.storage.Remove(trashPath); err != nil {
			return err
		}
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
	if item.Type != model.ItemTypeFile {
		return nil
	}
	// Best-effort, same as indexContent's own build side — a file that was
	// never actually indexed (unsupported type, or indexing itself once
	// failed) has nothing to remove here anyway.
	if err := s.search.RemoveContent(ctx, id); err != nil {
		log.Printf("search index: remove %s: %v", id, err)
	}
	return s.users.IncrementStorageUsed(ctx, ownerID, -item.SizeBytes)
}
