-- Marks which scanned (text-less) PDFs have already had an OCR pass run
-- against them by the background sweep (ItemService.RunOCRSweep) — its
-- only job is stopping that sweep from retrying the same blank-scan PDF
-- on every tick forever. It records that an attempt happened, not what it
-- found: whatever text OCR produced (even none) already lands in
-- item_content_fts via the normal indexContent path.
CREATE TABLE ocr_attempts (
    item_id      TEXT PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    attempted_at INTEGER NOT NULL
);
