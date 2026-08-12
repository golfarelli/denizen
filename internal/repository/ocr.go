package repository

import (
	"context"
	stdsql "database/sql"
	"strings"

	"github.com/golfarelli/denizen/internal/model"
	"github.com/golfarelli/denizen/internal/textextract"
)

// ocrExts is every extension the background OCR sweep can act on: scanned
// PDFs plus photographed documents (textextract.OCRImageExts) — kept as
// one list so ListPending's candidate query and
// ItemService.RunOCRSweep's dispatch can't quietly drift apart.
var ocrExts = append([]string{"pdf"}, textextract.OCRImageExts...)

// OCRRepository is the only code allowed to run SQL against ocr_attempts
// (internal/db/migrations/0005_ocr_attempts.sql). ListPending also reads
// items and item_content_fts, both owned by other repositories — a
// read-only join for candidate selection, same spirit as
// ItemRepository.SearchByNameInSubtree reaching across tables it doesn't
// own; writes to those two tables still only ever happen through
// ItemRepository/SearchRepository themselves.
type OCRRepository struct {
	cn *stdsql.DB
}

func NewOCRRepository(cn *stdsql.DB) *OCRRepository {
	return &OCRRepository{cn: cn}
}

// ListPending returns up to limit active PDF/photo file items (ocrExts)
// whose indexed content is empty — either genuinely never indexed (a
// photo: nothing else in textextract even attempts one, see Supported) or
// nothing but pdftotext's own page-break characters (form feed, \f —
// exactly what a scanned, text-less PDF produces; see textextract's own
// doc comment) — and that haven't had an OCR attempt recorded yet. Oldest
// first, so a long backlog drains in upload order rather than
// newest-first forever starving whatever was already waiting.
func (r *OCRRepository) ListPending(ctx context.Context, limit int) ([]*model.Item, error) {
	extClauses := make([]string, len(ocrExts))
	args := make([]any, 0, len(ocrExts)+1)
	for i, ext := range ocrExts {
		extClauses[i] = "lower(items.name) LIKE ?"
		args = append(args, "%."+ext)
	}
	args = append(args, limit)

	sql := `SELECT items.id, items.owner_id, items.parent_id, items.name, items.type, items.size_bytes,
	               items.mime_type, items.checksum, items.deleted_at, items.created_at, items.updated_at
	        FROM items
	        LEFT JOIN item_content_fts ON item_content_fts.item_id = items.id
	        WHERE items.type = 'file' AND items.deleted_at IS NULL
	          AND (` + strings.Join(extClauses, " OR ") + `)
	          AND NOT EXISTS (SELECT 1 FROM ocr_attempts WHERE ocr_attempts.item_id = items.id)
	          AND (item_content_fts.content IS NULL
	               OR trim(replace(item_content_fts.content, char(12), '')) = '')
	        ORDER BY items.created_at ASC
	        LIMIT ?`
	rows, err := r.cn.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAll(rows)
}

// MarkAttempted records that itemID just had an OCR pass run against it,
// regardless of outcome. Upserted rather than a plain INSERT: harmless if
// ClearAttempt+a fresh attempt ever race within the same sweep tick.
func (r *OCRRepository) MarkAttempted(ctx context.Context, itemID string, at int64) error {
	_, err := r.cn.ExecContext(ctx,
		`INSERT INTO ocr_attempts (item_id, attempted_at) VALUES (?, ?)
		 ON CONFLICT (item_id) DO UPDATE SET attempted_at = excluded.attempted_at`,
		itemID, at)
	return err
}

// ClearAttempt removes itemID's marker, if any — called when a file's
// bytes are replaced (ItemService.ReplaceContent) so a re-uploaded PDF
// gets a fresh OCR attempt instead of being permanently skipped over some
// earlier version of it once having come back blank.
func (r *OCRRepository) ClearAttempt(ctx context.Context, itemID string) error {
	_, err := r.cn.ExecContext(ctx, `DELETE FROM ocr_attempts WHERE item_id = ?`, itemID)
	return err
}
