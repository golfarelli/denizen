package repository

import (
	"context"
	stdsql "database/sql"
	"errors"

	"github.com/golfarelli/denizen/internal/model"
)

// ShareRepository is the only code allowed to run SQL against the `shares`
// table.
type ShareRepository struct {
	cn *stdsql.DB
}

func NewShareRepository(cn *stdsql.DB) *ShareRepository {
	return &ShareRepository{cn: cn}
}

func (r *ShareRepository) Create(ctx context.Context, item *model.Share) error {
	sql := `INSERT INTO shares (id, item_id, token_hash, created_by, requires_auth, expires_at, created_at)
	        VALUES (?, ?, ?, ?, ?, ?, ?)`
	_, err := r.cn.ExecContext(ctx, sql,
		item.ID, item.ItemID, item.TokenHash, item.CreatedBy, item.RequiresAuth, item.ExpiresAt, item.CreatedAt)
	return err
}

func (r *ShareRepository) GetByID(ctx context.Context, id string) (*model.Share, error) {
	sql := `SELECT id, item_id, token_hash, created_by, requires_auth, expires_at, created_at
	        FROM shares WHERE id = ?`
	return r.scanOne(r.cn.QueryRowContext(ctx, sql, id))
}

func (r *ShareRepository) GetByTokenHash(ctx context.Context, tokenHash string) (*model.Share, error) {
	sql := `SELECT id, item_id, token_hash, created_by, requires_auth, expires_at, created_at
	        FROM shares WHERE token_hash = ?`
	return r.scanOne(r.cn.QueryRowContext(ctx, sql, tokenHash))
}

// ListByOwner lists ownerID's own active share links, most recent first.
func (r *ShareRepository) ListByOwner(ctx context.Context, ownerID string) ([]*model.Share, error) {
	sql := `SELECT id, item_id, token_hash, created_by, requires_auth, expires_at, created_at
	        FROM shares WHERE created_by = ? ORDER BY created_at DESC`
	rows, err := r.cn.QueryContext(ctx, sql, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*model.Share
	for rows.Next() {
		item, err := scanShare(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// Delete revokes a share — a row delete, not a soft delete (see schema.sql).
func (r *ShareRepository) Delete(ctx context.Context, id string) error {
	sql := `DELETE FROM shares WHERE id = ?`
	_, err := r.cn.ExecContext(ctx, sql, id)
	return err
}

func (r *ShareRepository) scanOne(row *stdsql.Row) (*model.Share, error) {
	item, err := scanShare(row)
	if errors.Is(err, stdsql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return item, nil
}

func scanShare(row rowScanner) (*model.Share, error) {
	item := &model.Share{}
	var expiresAt stdsql.NullInt64
	err := row.Scan(&item.ID, &item.ItemID, &item.TokenHash, &item.CreatedBy,
		&item.RequiresAuth, &expiresAt, &item.CreatedAt)
	if err != nil {
		return nil, err
	}
	if expiresAt.Valid {
		item.ExpiresAt = &expiresAt.Int64
	}
	return item, nil
}
