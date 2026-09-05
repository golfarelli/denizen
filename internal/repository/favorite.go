package repository

import (
	"context"
	stdsql "database/sql"
	"strings"
)

// FavoriteRepository is the only code allowed to run SQL against the
// `item_favorites` table — see its own migration (0007_item_favorites.sql)
// for why this is a join table, not a column on items.
type FavoriteRepository struct {
	cn *stdsql.DB
}

func NewFavoriteRepository(cn *stdsql.DB) *FavoriteRepository {
	return &FavoriteRepository{cn: cn}
}

// Add records itemID as one of userID's favorites — idempotent (INSERT OR
// IGNORE against the (item_id, user_id) UNIQUE constraint), so favoriting
// an already-favorited item twice in a row (e.g. a doubled click) is a
// harmless no-op rather than an error the caller has to special-case.
func (r *FavoriteRepository) Add(ctx context.Context, id, itemID, userID string, createdAt int64) error {
	sql := `INSERT OR IGNORE INTO item_favorites (id, item_id, user_id, created_at) VALUES (?, ?, ?, ?)`
	_, err := r.cn.ExecContext(ctx, sql, id, itemID, userID, createdAt)
	return err
}

// Remove un-favorites itemID for userID — also idempotent (a DELETE that
// matches zero rows isn't an error), same reasoning as Add.
func (r *FavoriteRepository) Remove(ctx context.Context, itemID, userID string) error {
	sql := `DELETE FROM item_favorites WHERE item_id = ? AND user_id = ?`
	_, err := r.cn.ExecContext(ctx, sql, itemID, userID)
	return err
}

// ListItemIDs returns userID's favorited item ids, most recently favorited
// first — ItemService.ListFavorites resolves each into a real *model.Item
// (and re-checks current access for anything not owned by userID), this
// repository has no idea what an item actually is beyond its id.
func (r *FavoriteRepository) ListItemIDs(ctx context.Context, userID string) ([]string, error) {
	sql := `SELECT item_id FROM item_favorites WHERE user_id = ? ORDER BY created_at DESC`
	rows, err := r.cn.QueryContext(ctx, sql, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// FavoritedSet reports which of itemIDs are among userID's favorites —
// batch lookup for enriching a list response (ItemHandler's own
// enrichFavorites, same shape as enrichSharedWith) with one query instead
// of one round trip per row.
func (r *FavoriteRepository) FavoritedSet(ctx context.Context, userID string, itemIDs []string) (map[string]bool, error) {
	if len(itemIDs) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(itemIDs)), ",")
	sql := `SELECT item_id FROM item_favorites WHERE user_id = ? AND item_id IN (` + placeholders + `)`
	args := make([]any, 0, len(itemIDs)+1)
	args = append(args, userID)
	for _, id := range itemIDs {
		args = append(args, id)
	}
	rows, err := r.cn.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	set := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		set[id] = true
	}
	return set, rows.Err()
}
