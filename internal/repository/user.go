package repository

import (
	"context"
	stdsql "database/sql"
	"errors"

	"github.com/golfarelli/denizen/internal/model"
)

// UserRepository is the only code allowed to run SQL against the `users`
// table.
type UserRepository struct {
	cn *stdsql.DB
}

func NewUserRepository(cn *stdsql.DB) *UserRepository {
	return &UserRepository{cn: cn}
}

func (r *UserRepository) Create(ctx context.Context, item *model.User) error {
	sql := `INSERT INTO users (id, username, password_hash, is_admin, quota_bytes, storage_used_bytes, disabled, created_at)
	        VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := r.cn.ExecContext(ctx, sql,
		item.ID, item.Username, item.PasswordHash, item.IsAdmin,
		item.QuotaBytes, item.StorageUsedBytes, item.Disabled, item.CreatedAt)
	return err
}

func (r *UserRepository) GetByUsername(ctx context.Context, username string) (*model.User, error) {
	sql := `SELECT id, username, password_hash, is_admin, quota_bytes, storage_used_bytes, disabled, created_at
	        FROM users WHERE username = ?`
	return r.scanOne(r.cn.QueryRowContext(ctx, sql, username))
}

func (r *UserRepository) GetByID(ctx context.Context, id string) (*model.User, error) {
	sql := `SELECT id, username, password_hash, is_admin, quota_bytes, storage_used_bytes, disabled, created_at
	        FROM users WHERE id = ?`
	return r.scanOne(r.cn.QueryRowContext(ctx, sql, id))
}

// CountAll is used only to detect a fresh install (zero users → offer the
// bootstrap invite instead of requiring one that doesn't exist yet).
func (r *UserRepository) CountAll(ctx context.Context) (int, error) {
	var count int
	sql := `SELECT count(*) FROM users`
	err := r.cn.QueryRowContext(ctx, sql).Scan(&count)
	return count, err
}

func (r *UserRepository) scanOne(row *stdsql.Row) (*model.User, error) {
	item := &model.User{}
	err := row.Scan(&item.ID, &item.Username, &item.PasswordHash, &item.IsAdmin,
		&item.QuotaBytes, &item.StorageUsedBytes, &item.Disabled, &item.CreatedAt)
	if errors.Is(err, stdsql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return item, nil
}
