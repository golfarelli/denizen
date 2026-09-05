package repository

import (
	"context"
	stdsql "database/sql"
	"errors"
	"strings"

	"github.com/golfarelli/denizen/internal/model"
)

// UserShareRepository is the only code allowed to run SQL against the
// `user_shares` table.
type UserShareRepository struct {
	cn *stdsql.DB
}

func NewUserShareRepository(cn *stdsql.DB) *UserShareRepository {
	return &UserShareRepository{cn: cn}
}

func (r *UserShareRepository) Create(ctx context.Context, item *model.UserShare) error {
	sql := `INSERT INTO user_shares (id, item_id, owner_id, shared_with_id, permission, created_at)
	        VALUES (?, ?, ?, ?, ?, ?)`
	_, err := r.cn.ExecContext(ctx, sql, item.ID, item.ItemID, item.OwnerID, item.SharedWithID, item.Permission, item.CreatedAt)
	return err
}

func (r *UserShareRepository) GetByID(ctx context.Context, id string) (*model.UserShare, error) {
	sql := `SELECT id, item_id, owner_id, shared_with_id, permission, created_at FROM user_shares WHERE id = ?`
	return r.scanOne(r.cn.QueryRowContext(ctx, sql, id))
}

// FindGrant returns the direct grant of itemID to userID, or ErrNotFound if
// there isn't one — the permission-aware replacement for what used to be a
// plain boolean Exists check. ItemService.resolveGrant is the only caller
// that matters: it calls this once per ancestor while walking up from an
// arbitrary item to find the nearest folder (or the item itself) that's
// actually been shared with someone.
func (r *UserShareRepository) FindGrant(ctx context.Context, itemID, userID string) (*model.UserShare, error) {
	sql := `SELECT id, item_id, owner_id, shared_with_id, permission, created_at
	        FROM user_shares WHERE item_id = ? AND shared_with_id = ?`
	return r.scanOne(r.cn.QueryRowContext(ctx, sql, itemID, userID))
}

// ListByItem lists everyone itemID has been directly shared with — for the
// owner's own "who has access to this" view (ShareDialog's people
// section).
func (r *UserShareRepository) ListByItem(ctx context.Context, itemID string) ([]*model.UserShare, error) {
	sql := `SELECT id, item_id, owner_id, shared_with_id, permission, created_at
	        FROM user_shares WHERE item_id = ? ORDER BY created_at ASC`
	rows, err := r.cn.QueryContext(ctx, sql, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAllUserShares(rows)
}

// ListByOwner lists every grant ownerID has ever made, across all of their
// items, most recent first — the "My shares" page's own source of truth
// for direct shares (routes/shares/+page.svelte previously only showed
// token-based links; this is its counterpart for people-shares).
func (r *UserShareRepository) ListByOwner(ctx context.Context, ownerID string) ([]*model.UserShare, error) {
	sql := `SELECT id, item_id, owner_id, shared_with_id, permission, created_at
	        FROM user_shares WHERE owner_id = ? ORDER BY created_at DESC`
	rows, err := r.cn.QueryContext(ctx, sql, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAllUserShares(rows)
}

// ListByItems is ListByItem for many items at once — the file browser's
// "who has access" badge on each row needs one grant list per visible
// item, and doing that as N round trips instead of one gets slow the
// moment a folder has more than a handful of shared entries in it.
func (r *UserShareRepository) ListByItems(ctx context.Context, itemIDs []string) ([]*model.UserShare, error) {
	if len(itemIDs) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(itemIDs)), ",")
	sql := `SELECT id, item_id, owner_id, shared_with_id, permission, created_at
	        FROM user_shares WHERE item_id IN (` + placeholders + `) ORDER BY created_at ASC`
	args := make([]any, len(itemIDs))
	for i, id := range itemIDs {
		args[i] = id
	}
	rows, err := r.cn.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAllUserShares(rows)
}

// ListReceivedBy lists every item directly shared with userID, most recent
// first — the "Shared with me" page's own source of truth.
func (r *UserShareRepository) ListReceivedBy(ctx context.Context, userID string) ([]*model.UserShare, error) {
	sql := `SELECT id, item_id, owner_id, shared_with_id, permission, created_at
	        FROM user_shares WHERE shared_with_id = ? ORDER BY created_at DESC`
	rows, err := r.cn.QueryContext(ctx, sql, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAllUserShares(rows)
}

// UpdatePermission changes an existing grant's access level in place —
// same id, same recipient, just view <-> edit.
func (r *UserShareRepository) UpdatePermission(ctx context.Context, id string, permission model.SharePermission) error {
	sql := `UPDATE user_shares SET permission = ? WHERE id = ?`
	_, err := r.cn.ExecContext(ctx, sql, permission, id)
	return err
}

// Delete revokes a grant — a row delete, not a soft delete, same
// reasoning as the token-based Share (see ShareRepository.Delete).
func (r *UserShareRepository) Delete(ctx context.Context, id string) error {
	sql := `DELETE FROM user_shares WHERE id = ?`
	_, err := r.cn.ExecContext(ctx, sql, id)
	return err
}

func scanAllUserShares(rows *stdsql.Rows) ([]*model.UserShare, error) {
	var items []*model.UserShare
	for rows.Next() {
		item, err := scanUserShare(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *UserShareRepository) scanOne(row *stdsql.Row) (*model.UserShare, error) {
	item, err := scanUserShare(row)
	if errors.Is(err, stdsql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return item, nil
}

func scanUserShare(row rowScanner) (*model.UserShare, error) {
	item := &model.UserShare{}
	err := row.Scan(&item.ID, &item.ItemID, &item.OwnerID, &item.SharedWithID, &item.Permission, &item.CreatedAt)
	if err != nil {
		return nil, err
	}
	return item, nil
}
