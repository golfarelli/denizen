package repository

import (
	"context"
	stdsql "database/sql"
	"errors"

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
	sql := `INSERT INTO user_shares (id, item_id, owner_id, shared_with_id, created_at)
	        VALUES (?, ?, ?, ?, ?)`
	_, err := r.cn.ExecContext(ctx, sql, item.ID, item.ItemID, item.OwnerID, item.SharedWithID, item.CreatedAt)
	return err
}

func (r *UserShareRepository) GetByID(ctx context.Context, id string) (*model.UserShare, error) {
	sql := `SELECT id, item_id, owner_id, shared_with_id, created_at FROM user_shares WHERE id = ?`
	return r.scanOne(r.cn.QueryRowContext(ctx, sql, id))
}

// Exists reports whether itemID has been directly shared with userID —
// the only check ItemService.GetIncludingTrashed needs to decide whether a
// non-owner may read a file.
func (r *UserShareRepository) Exists(ctx context.Context, itemID, userID string) (bool, error) {
	var count int
	sql := `SELECT count(*) FROM user_shares WHERE item_id = ? AND shared_with_id = ?`
	if err := r.cn.QueryRowContext(ctx, sql, itemID, userID).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

// ListByItem lists everyone itemID has been directly shared with — for the
// owner's own "who has access to this" view (ShareDialog's people
// section).
func (r *UserShareRepository) ListByItem(ctx context.Context, itemID string) ([]*model.UserShare, error) {
	sql := `SELECT id, item_id, owner_id, shared_with_id, created_at
	        FROM user_shares WHERE item_id = ? ORDER BY created_at ASC`
	rows, err := r.cn.QueryContext(ctx, sql, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAllUserShares(rows)
}

// ListReceivedBy lists every item directly shared with userID, most recent
// first — the "Shared with me" page's own source of truth.
func (r *UserShareRepository) ListReceivedBy(ctx context.Context, userID string) ([]*model.UserShare, error) {
	sql := `SELECT id, item_id, owner_id, shared_with_id, created_at
	        FROM user_shares WHERE shared_with_id = ? ORDER BY created_at DESC`
	rows, err := r.cn.QueryContext(ctx, sql, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAllUserShares(rows)
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
	err := row.Scan(&item.ID, &item.ItemID, &item.OwnerID, &item.SharedWithID, &item.CreatedAt)
	if err != nil {
		return nil, err
	}
	return item, nil
}
