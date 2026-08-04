package repository

import (
	"context"
	stdsql "database/sql"
	"errors"

	"github.com/golfarelli/denizen/internal/model"
)

// RefreshTokenRepository is the only code allowed to run SQL against the
// `refresh_tokens` table.
type RefreshTokenRepository struct {
	cn *stdsql.DB
}

func NewRefreshTokenRepository(cn *stdsql.DB) *RefreshTokenRepository {
	return &RefreshTokenRepository{cn: cn}
}

func (r *RefreshTokenRepository) Create(ctx context.Context, item *model.RefreshToken) error {
	sql := `INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at, revoked_at, created_at)
	        VALUES (?, ?, ?, ?, ?, ?)`
	_, err := r.cn.ExecContext(ctx, sql,
		item.ID, item.UserID, item.TokenHash, item.ExpiresAt, item.RevokedAt, item.CreatedAt)
	return err
}

func (r *RefreshTokenRepository) GetByHash(ctx context.Context, tokenHash string) (*model.RefreshToken, error) {
	sql := `SELECT id, user_id, token_hash, expires_at, revoked_at, created_at
	        FROM refresh_tokens WHERE token_hash = ?`
	row := r.cn.QueryRowContext(ctx, sql, tokenHash)
	item := &model.RefreshToken{}
	err := row.Scan(&item.ID, &item.UserID, &item.TokenHash, &item.ExpiresAt, &item.RevokedAt, &item.CreatedAt)
	if errors.Is(err, stdsql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (r *RefreshTokenRepository) Revoke(ctx context.Context, id string, revokedAt int64) error {
	sql := `UPDATE refresh_tokens SET revoked_at = ? WHERE id = ?`
	_, err := r.cn.ExecContext(ctx, sql, revokedAt, id)
	return err
}
