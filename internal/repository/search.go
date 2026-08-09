package repository

import (
	"context"
	stdsql "database/sql"
	"strings"
)

// SearchRepository is the only code allowed to run SQL against the
// item_content_fts virtual table (internal/db/migrations/
// 0004_content_search.sql) — a standalone FTS5 index, not SQLite's
// "external content" mode: at this app's scale (a personal/family drive,
// not a document management system), a plain delete-then-reinsert on
// every re-index is simpler than keeping an external-content FTS5 table's
// rowid mapping in sync with the items table across every rename/move/
// delete. It only ever stores item_id + extracted text — no ownership or
// trash-state columns, both cross-checked against the real items table by
// ItemService.Search after querying this one.
type SearchRepository struct {
	cn *stdsql.DB
}

func NewSearchRepository(cn *stdsql.DB) *SearchRepository {
	return &SearchRepository{cn: cn}
}

// IndexContent replaces itemID's indexed content with text — called after
// every successful upload/content edit (see ItemService.indexContent).
// Delete-then-insert rather than UPDATE: FTS5 has no notion of "the row
// for this item_id" to update in place without external-content mode (see
// this type's own doc comment), and item_id isn't unique-constrained here
// (FTS5 doesn't support it), so without the delete a re-index would just
// accumulate duplicate rows for the same file.
func (r *SearchRepository) IndexContent(ctx context.Context, itemID, content string) error {
	tx, err := r.cn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op once Commit succeeds

	if _, err := tx.ExecContext(ctx, `DELETE FROM item_content_fts WHERE item_id = ?`, itemID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO item_content_fts (item_id, content) VALUES (?, ?)`, itemID, content); err != nil {
		return err
	}
	return tx.Commit()
}

// RemoveContent deletes itemID's indexed content, if any — called once a
// file's bytes are actually gone for good (ItemService.hardDeleteSubtreeRows),
// not on a soft delete: a trashed file stays searchable-in-principle here,
// ItemService.Search filters it out via the real items table's deleted_at
// instead, the same way a name-match search result would be.
func (r *SearchRepository) RemoveContent(ctx context.Context, itemID string) error {
	_, err := r.cn.ExecContext(ctx, `DELETE FROM item_content_fts WHERE item_id = ?`, itemID)
	return err
}

// SearchContent returns the item ids whose indexed content matches query,
// best FTS5 rank first. query is tokenized defensively (see
// sanitizeFTS5Query) so arbitrary user input — a stray `"`, `*`, `-`, or
// other FTS5 query-syntax character — can't turn into a MATCH syntax
// error instead of a search.
func (r *SearchRepository) SearchContent(ctx context.Context, query string, limit int) ([]string, error) {
	matchQuery := sanitizeFTS5Query(query)
	if matchQuery == "" {
		return nil, nil
	}
	rows, err := r.cn.QueryContext(ctx,
		`SELECT item_id FROM item_content_fts WHERE item_content_fts MATCH ? ORDER BY rank LIMIT ?`,
		matchQuery, limit)
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

// sanitizeFTS5Query turns free-typed user input into a safe FTS5 MATCH
// expression: each whitespace-separated term, double-quoted (escaping any
// literal `"` by doubling it, FTS5's own quoting rule) and suffixed with
// `*` for prefix matching ("bollet" finds "bolletta" while typing), joined
// with implicit AND — every term must appear somewhere in the document.
// Without this, raw user text containing FTS5 operators (a bare `"`,
// `AND`/`OR`/`NOT`, `*`, `-`, `(`/`)`) either errors the query outright or
// silently changes its meaning in ways a search box should never expose.
func sanitizeFTS5Query(query string) string {
	terms := strings.Fields(query)
	quoted := make([]string, 0, len(terms))
	for _, term := range terms {
		term = strings.ReplaceAll(term, `"`, `""`)
		quoted = append(quoted, `"`+term+`"*`)
	}
	return strings.Join(quoted, " ")
}
