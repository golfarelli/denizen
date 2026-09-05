package repository

import (
	"context"
	stdsql "database/sql"
	"errors"

	"github.com/golfarelli/denizen/internal/model"
)

// InviteRepository is the only code allowed to run SQL against the
// `invites` table.
type InviteRepository struct {
	cn *stdsql.DB
}

func NewInviteRepository(cn *stdsql.DB) *InviteRepository {
	return &InviteRepository{cn: cn}
}

func (r *InviteRepository) Create(ctx context.Context, item *model.Invite) error {
	sql := `INSERT INTO invites (id, code, created_by, grants_admin, quota_bytes, expires_at, used_at, used_by, created_at)
	        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	createdBy := stdsql.NullString{String: item.CreatedBy, Valid: item.CreatedBy != ""}
	_, err := r.cn.ExecContext(ctx, sql,
		item.ID, item.Code, createdBy, item.GrantsAdmin, item.QuotaBytes,
		item.ExpiresAt, item.UsedAt, item.UsedBy, item.CreatedAt)
	return err
}

func (r *InviteRepository) GetByCode(ctx context.Context, code string) (*model.Invite, error) {
	sql := `SELECT id, code, created_by, grants_admin, quota_bytes, expires_at, used_at, used_by, created_at
	        FROM invites WHERE code = ?`
	row := r.cn.QueryRowContext(ctx, sql, code)
	item := &model.Invite{}
	var createdBy stdsql.NullString
	err := row.Scan(&item.ID, &item.Code, &createdBy, &item.GrantsAdmin, &item.QuotaBytes,
		&item.ExpiresAt, &item.UsedAt, &item.UsedBy, &item.CreatedAt)
	if errors.Is(err, stdsql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	item.CreatedBy = createdBy.String
	return item, nil
}

// MarkUsed atomically marks the invite as redeemed by userID, but only if it
// hasn't been redeemed already — the "AND used_at IS NULL" guards the race
// between two requests trying to redeem the same code at once. Returns
// false (no error) if the invite was already used by the time this ran.
func (r *InviteRepository) MarkUsed(ctx context.Context, code, userID string, usedAt int64) (bool, error) {
	sql := `UPDATE invites SET used_at = ?, used_by = ? WHERE code = ? AND used_at IS NULL`
	res, err := r.cn.ExecContext(ctx, sql, usedAt, userID, code)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}
