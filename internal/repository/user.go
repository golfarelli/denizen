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

// IncrementStorageUsed adjusts a user's storage_used_bytes counter by delta
// (negative to free space, e.g. on permanent deletion). Kept as a simple
// increment rather than a recomputed SUM(size_bytes) over all items, so it
// stays cheap regardless of how many files a user has.
func (r *UserRepository) IncrementStorageUsed(ctx context.Context, userID string, delta int64) error {
	sql := `UPDATE users SET storage_used_bytes = storage_used_bytes + ? WHERE id = ?`
	_, err := r.cn.ExecContext(ctx, sql, delta, userID)
	return err
}

// ListAll lists every user — admin-only (see internal/handler.UserHandler).
func (r *UserRepository) ListAll(ctx context.Context) ([]*model.User, error) {
	sql := `SELECT id, username, password_hash, is_admin, quota_bytes, storage_used_bytes, disabled, created_at
	        FROM users ORDER BY created_at ASC`
	rows, err := r.cn.QueryContext(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*model.User
	for rows.Next() {
		item, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// UpdateQuotaAndDisabled sets both fields at once — the service layer reads
// the current row first and fills in whichever of the two the caller didn't
// ask to change, since the PATCH endpoint accepts either independently
// (see internal/service.UserService.Update).
func (r *UserRepository) UpdateQuotaAndDisabled(ctx context.Context, id string, quotaBytes int64, disabled bool) error {
	sql := `UPDATE users SET quota_bytes = ?, disabled = ? WHERE id = ?`
	_, err := r.cn.ExecContext(ctx, sql, quotaBytes, disabled, id)
	return err
}

func (r *UserRepository) scanOne(row *stdsql.Row) (*model.User, error) {
	item, err := scanUser(row)
	if errors.Is(err, stdsql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return item, nil
}

func scanUser(row rowScanner) (*model.User, error) {
	item := &model.User{}
	err := row.Scan(&item.ID, &item.Username, &item.PasswordHash, &item.IsAdmin,
		&item.QuotaBytes, &item.StorageUsedBytes, &item.Disabled, &item.CreatedAt)
	if err != nil {
		return nil, err
	}
	return item, nil
}
