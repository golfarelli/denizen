package repository

import (
	"context"
	stdsql "database/sql"

	"github.com/golfarelli/denizen/internal/model"
)

// OCRRepository is the only code allowed to run SQL against ocr_queue and
// ocr_attempts (internal/db/migrations/0005_ocr_attempts.sql, 0008_ocr_queue.sql).
// ListPending also reads items, owned by another repository — a read-only
// join for candidate selection, same spirit as
// ItemRepository.SearchByNameInSubtree reaching across tables it doesn't
// own.
type OCRRepository struct {
	cn *stdsql.DB
}

func NewOCRRepository(cn *stdsql.DB) *OCRRepository {
	return &OCRRepository{cn: cn}
}

// ListPending returns up to limit active file items waiting in ocr_queue,
// oldest first, so a long backlog drains in the order files were queued
// rather than newest-first forever starving whatever was already waiting.
//
// This used to work out "still needs OCR?" on every call by joining
// item_content_fts and testing that the indexed text was empty — which
// meant reading the full indexed content of every PDF/photo without an
// attempt marker, every time (see 0008_ocr_queue.sql). Whether a file needs
// OCR is now decided once, when it's indexed (ItemService.indexContent →
// SetQueued), and this is just a read of the resulting small table.
func (r *OCRRepository) ListPending(ctx context.Context, limit int) ([]*model.Item, error) {
	rows, err := r.cn.QueryContext(ctx,
		`SELECT items.id, items.owner_id, items.parent_id, items.name, items.type, items.size_bytes,
		        items.mime_type, items.checksum, items.target_id, items.deleted_at, items.created_at, items.updated_at
		 FROM ocr_queue
		 JOIN items ON items.id = ocr_queue.item_id
		 WHERE items.type = 'file' AND items.deleted_at IS NULL AND items.target_id IS NULL
		 ORDER BY ocr_queue.queued_at ASC, items.created_at ASC
		 LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAll(rows)
}

// SetQueued records whether itemID needs an OCR pass. queued=true adds it
// to the queue — unless it already had an attempt (a blank scan the sweep
// gave up on stays given-up-on until ClearAttempt says the bytes changed) —
// and queued=false removes it (e.g. a replaced file whose new version has a
// real text layer, so an OCR pass queued for the old version is moot).
func (r *OCRRepository) SetQueued(ctx context.Context, itemID string, queued bool, at int64) error {
	if !queued {
		_, err := r.cn.ExecContext(ctx, `DELETE FROM ocr_queue WHERE item_id = ?`, itemID)
		return err
	}
	_, err := r.cn.ExecContext(ctx,
		`INSERT OR IGNORE INTO ocr_queue (item_id, queued_at)
		 SELECT ?, ? WHERE NOT EXISTS (SELECT 1 FROM ocr_attempts WHERE item_id = ?)`,
		itemID, at, itemID)
	return err
}

// MarkAttempted records that itemID just had an OCR pass run against it,
// regardless of outcome, and takes it off the queue. Upserted rather than
// a plain INSERT: harmless if ClearAttempt+a fresh attempt ever race within
// the same sweep tick.
func (r *OCRRepository) MarkAttempted(ctx context.Context, itemID string, at int64) error {
	tx, err := r.cn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO ocr_attempts (item_id, attempted_at) VALUES (?, ?)
		 ON CONFLICT (item_id) DO UPDATE SET attempted_at = excluded.attempted_at`,
		itemID, at); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM ocr_queue WHERE item_id = ?`, itemID); err != nil {
		return err
	}
	return tx.Commit()
}

// ClearAttempt removes itemID's marker, if any — called when a file's
// bytes are replaced (ItemService.ReplaceContent) so a re-uploaded PDF
// gets a fresh OCR attempt instead of being permanently skipped over some
// earlier version of it once having come back blank.
func (r *OCRRepository) ClearAttempt(ctx context.Context, itemID string) error {
	_, err := r.cn.ExecContext(ctx, `DELETE FROM ocr_attempts WHERE item_id = ?`, itemID)
	return err
}
