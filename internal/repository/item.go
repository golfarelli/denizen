package repository

import (
	"context"
	stdsql "database/sql"
	"errors"
	"strings"

	"github.com/golfarelli/denizen/internal/model"
)

// ItemRepository is the only code allowed to run SQL against the `items`
// table.
type ItemRepository struct {
	cn *stdsql.DB
}

func NewItemRepository(cn *stdsql.DB) *ItemRepository {
	return &ItemRepository{cn: cn}
}

func (r *ItemRepository) Create(ctx context.Context, item *model.Item) error {
	sql := `INSERT INTO items (id, owner_id, parent_id, name, type, size_bytes, mime_type, checksum, deleted_at, created_at, updated_at)
	        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := r.cn.ExecContext(ctx, sql,
		item.ID, item.OwnerID, item.ParentID, item.Name, string(item.Type),
		item.SizeBytes, item.MimeType, item.Checksum, item.DeletedAt, item.CreatedAt, item.UpdatedAt)
	return err
}

// GetByID returns the item regardless of whether it's active or in trash —
// callers that only want one or the other (nearly everyone) check
// item.DeletedAt themselves.
func (r *ItemRepository) GetByID(ctx context.Context, id string) (*model.Item, error) {
	sql := `SELECT id, owner_id, parent_id, name, type, size_bytes, mime_type, checksum, deleted_at, created_at, updated_at
	        FROM items WHERE id = ?`
	return r.scanOne(r.cn.QueryRowContext(ctx, sql, id))
}

// GetByIDs is GetByID for many ids at once, silently skipping any that
// don't exist (a content search hit whose item was since hard-deleted —
// see ItemService.Search) rather than erroring the whole batch over it.
// Order isn't guaranteed to match ids; callers that care (Search does, for
// content-match ranking) re-order after the fact.
func (r *ItemRepository) GetByIDs(ctx context.Context, ids []string) ([]*model.Item, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	sql := `SELECT id, owner_id, parent_id, name, type, size_bytes, mime_type, checksum, deleted_at, created_at, updated_at
	        FROM items WHERE id IN (` + placeholders + `)`
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := r.cn.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAll(rows)
}

// likeEscape escapes query for a LIKE ... ESCAPE '\' pattern (SQLite's own
// LIKE has no special handling of these, they'd otherwise be interpreted
// as wildcards or break the pattern) — shared by SearchByName and
// SearchByNameInSubtree below.
func likeEscape(query string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
}

// SearchByName lists ownerID's own active items whose name contains query
// (case-insensitive — SQLite's LIKE already folds ASCII case by default),
// most-recently-modified first. The whole-tree counterpart to
// ListChildren's one-folder-at-a-time listing — GET /api/v1/search's name-
// match half (see ItemService.Search for how it's combined with a content
// match via internal/repository/search.go, and with SearchByNameInSubtree
// below for anything shared with the caller rather than owned by them).
func (r *ItemRepository) SearchByName(ctx context.Context, ownerID, query string, limit int) ([]*model.Item, error) {
	sql := `SELECT id, owner_id, parent_id, name, type, size_bytes, mime_type, checksum, deleted_at, created_at, updated_at
	        FROM items WHERE owner_id = ? AND deleted_at IS NULL AND name LIKE ? ESCAPE '\'
	        ORDER BY updated_at DESC LIMIT ?`
	rows, err := r.cn.QueryContext(ctx, sql, ownerID, "%"+likeEscape(query)+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAll(rows)
}

// SearchByNameInSubtree is SearchByName scoped to one subtree instead of
// one owner: rootID itself plus every active descendant of it (a
// recursive walk down parent_id, the mirror image of resolveGrant's own
// walk up it), filtered by name. rootID works whether it's a file (the
// recursive step just adds nothing, the base case still checks rootID
// itself) or a folder — ItemService.Search calls this once per item
// directly shared with the caller (UserShareRepository.ListReceivedBy),
// since a folder grant is inherited by everything nested inside it, same
// as browsing.
func (r *ItemRepository) SearchByNameInSubtree(ctx context.Context, rootID, query string, limit int) ([]*model.Item, error) {
	sql := `WITH RECURSIVE subtree(id) AS (
	            SELECT ?
	            UNION ALL
	            SELECT items.id FROM items JOIN subtree ON items.parent_id = subtree.id WHERE items.deleted_at IS NULL
	        )
	        SELECT items.id, items.owner_id, items.parent_id, items.name, items.type, items.size_bytes,
	               items.mime_type, items.checksum, items.deleted_at, items.created_at, items.updated_at
	        FROM items JOIN subtree ON items.id = subtree.id
	        WHERE items.deleted_at IS NULL AND items.name LIKE ? ESCAPE '\'
	        ORDER BY items.updated_at DESC LIMIT ?`
	rows, err := r.cn.QueryContext(ctx, sql, rootID, "%"+likeEscape(query)+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAll(rows)
}

// ListChildren lists the active (non-trashed) direct children of parentID
// (nil = the owner's root) owned by ownerID.
func (r *ItemRepository) ListChildren(ctx context.Context, ownerID string, parentID *string) ([]*model.Item, error) {
	sql := `SELECT id, owner_id, parent_id, name, type, size_bytes, mime_type, checksum, deleted_at, created_at, updated_at
	        FROM items WHERE owner_id = ? AND parent_id IS ? AND deleted_at IS NULL
	        ORDER BY type DESC, name ASC` // folders before files, then alphabetical
	rows, err := r.cn.QueryContext(ctx, sql, ownerID, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAll(rows)
}

// ListTrash lists ownerID's trashed items, most recently deleted first.
func (r *ItemRepository) ListTrash(ctx context.Context, ownerID string) ([]*model.Item, error) {
	sql := `SELECT id, owner_id, parent_id, name, type, size_bytes, mime_type, checksum, deleted_at, created_at, updated_at
	        FROM items WHERE owner_id = ? AND deleted_at IS NOT NULL
	        ORDER BY deleted_at DESC`
	rows, err := r.cn.QueryContext(ctx, sql, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAll(rows)
}

// ListTrashedBefore lists every trashed item, across all owners, whose
// deleted_at is older than cutoff — the system-wide counterpart to
// ListTrash (which is scoped to one owner), used only by the scheduled
// auto-purge sweep (see ItemService.PurgeExpiredTrash).
func (r *ItemRepository) ListTrashedBefore(ctx context.Context, cutoff int64) ([]*model.Item, error) {
	sql := `SELECT id, owner_id, parent_id, name, type, size_bytes, mime_type, checksum, deleted_at, created_at, updated_at
	        FROM items WHERE deleted_at IS NOT NULL AND deleted_at < ?
	        ORDER BY deleted_at ASC`
	rows, err := r.cn.QueryContext(ctx, sql, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAll(rows)
}

// ListChildrenDeleted lists the trashed direct children of parentID —
// the mirror image of ListChildren, used to walk down a trashed folder's
// subtree (e.g. while restoring it — see internal/service/item.go).
func (r *ItemRepository) ListChildrenDeleted(ctx context.Context, ownerID string, parentID *string) ([]*model.Item, error) {
	sql := `SELECT id, owner_id, parent_id, name, type, size_bytes, mime_type, checksum, deleted_at, created_at, updated_at
	        FROM items WHERE owner_id = ? AND parent_id IS ? AND deleted_at IS NOT NULL
	        ORDER BY name ASC`
	rows, err := r.cn.QueryContext(ctx, sql, ownerID, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAll(rows)
}

// NameExists reports whether an active item named `name` already exists
// directly under parentID for ownerID, other than excludeID itself — used
// to auto-suffix on collision before either the filesystem or the unique
// index ever sees a conflict. excludeID is the item being renamed/moved/
// restored (pass "" when creating something brand new — no real row has
// that id, so nothing is excluded).
func (r *ItemRepository) NameExists(ctx context.Context, ownerID string, parentID *string, name, excludeID string) (bool, error) {
	sql := `SELECT count(*) FROM items WHERE owner_id = ? AND parent_id IS ? AND name = ? AND deleted_at IS NULL AND id != ?`
	var count int
	err := r.cn.QueryRowContext(ctx, sql, ownerID, parentID, name, excludeID).Scan(&count)
	return count > 0, err
}

// UpdateNameParent renames and/or moves an item — a real filesystem
// rename/move should happen alongside this, before it, in the same
// service-layer operation (see internal/service/item.go).
func (r *ItemRepository) UpdateNameParent(ctx context.Context, id, name string, parentID *string, updatedAt int64) error {
	sql := `UPDATE items SET name = ?, parent_id = ?, updated_at = ? WHERE id = ?`
	_, err := r.cn.ExecContext(ctx, sql, name, parentID, updatedAt, id)
	return err
}

// UpdateContent updates a file item's content metadata after its bytes on
// disk were replaced in place — name and parent are untouched, unlike
// UpdateNameParent (see internal/service.ReplaceContent, used by the
// OnlyOffice save callback).
func (r *ItemRepository) UpdateContent(ctx context.Context, id string, sizeBytes int64, checksum string, updatedAt int64) error {
	sql := `UPDATE items SET size_bytes = ?, checksum = ?, updated_at = ? WHERE id = ?`
	_, err := r.cn.ExecContext(ctx, sql, sizeBytes, checksum, updatedAt, id)
	return err
}

// SoftDelete moves a single item into the trash (in the database sense —
// deleted_at is set; the actual file move happens in the service layer,
// which is also responsible for calling this for every item in a trashed
// folder's subtree, not just the top one).
func (r *ItemRepository) SoftDelete(ctx context.Context, id string, deletedAt int64) error {
	sql := `UPDATE items SET deleted_at = ? WHERE id = ?`
	_, err := r.cn.ExecContext(ctx, sql, deletedAt, id)
	return err
}

// Restore takes the top-level item of a restored subtree out of the trash,
// updating its name/parent (the restore may have renamed it on collision,
// or reparented it to root if its original parent is gone — see
// internal/service). Descendants use ClearDeletedAt instead, since their
// name and parent within the subtree don't change.
func (r *ItemRepository) Restore(ctx context.Context, id, name string, parentID *string, updatedAt int64) error {
	sql := `UPDATE items SET deleted_at = NULL, name = ?, parent_id = ?, updated_at = ? WHERE id = ?`
	_, err := r.cn.ExecContext(ctx, sql, name, parentID, updatedAt, id)
	return err
}

// ClearDeletedAt takes a single descendant out of the trash without
// touching its name or parent_id — see Restore.
func (r *ItemRepository) ClearDeletedAt(ctx context.Context, id string, updatedAt int64) error {
	sql := `UPDATE items SET deleted_at = NULL, updated_at = ? WHERE id = ?`
	_, err := r.cn.ExecContext(ctx, sql, updatedAt, id)
	return err
}

// HardDelete permanently removes an item's row — only ever called for an
// item already in the trash (deleted_at set), after its bytes are gone from
// disk too.
func (r *ItemRepository) HardDelete(ctx context.Context, id string) error {
	sql := `DELETE FROM items WHERE id = ?`
	_, err := r.cn.ExecContext(ctx, sql, id)
	return err
}

// ListAllFiles returns every active (non-trashed) file item across all
// owners — used only by the one-off content-search backfill (see
// ItemService.ReindexAllContent), everything else scopes to one owner.
func (r *ItemRepository) ListAllFiles(ctx context.Context) ([]*model.Item, error) {
	sql := `SELECT id, owner_id, parent_id, name, type, size_bytes, mime_type, checksum, deleted_at, created_at, updated_at
	        FROM items WHERE type = 'file' AND deleted_at IS NULL`
	rows, err := r.cn.QueryContext(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAll(rows)
}

func scanAll(rows *stdsql.Rows) ([]*model.Item, error) {
	var items []*model.Item
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *ItemRepository) scanOne(row *stdsql.Row) (*model.Item, error) {
	item, err := scanItem(row)
	if errors.Is(err, stdsql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return item, nil
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows, so scanItem works
// for GetByID's single-row path and the list queries' multi-row path alike.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanItem(row rowScanner) (*model.Item, error) {
	item := &model.Item{}
	var (
		parentID  stdsql.NullString
		itemType  string
		mimeType  stdsql.NullString
		checksum  stdsql.NullString
		deletedAt stdsql.NullInt64
	)
	err := row.Scan(&item.ID, &item.OwnerID, &parentID, &item.Name, &itemType,
		&item.SizeBytes, &mimeType, &checksum, &deletedAt, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil, err
	}
	item.Type = model.ItemType(itemType)
	if parentID.Valid {
		item.ParentID = &parentID.String
	}
	if mimeType.Valid {
		item.MimeType = &mimeType.String
	}
	if checksum.Valid {
		item.Checksum = &checksum.String
	}
	if deletedAt.Valid {
		item.DeletedAt = &deletedAt.Int64
	}
	return item, nil
}
