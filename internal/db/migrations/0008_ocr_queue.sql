-- The OCR sweep's work queue: files that still need an OCR pass.
--
-- Until now "does this file still need OCR?" was answered on every sweep by
-- OCRRepository.ListPending joining items against item_content_fts and
-- checking that the indexed text was empty. item_content_fts.item_id is an
-- UNINDEXED FTS5 column, so that join reads the full indexed content of
-- every PDF/photo that has no ocr_attempts row (any PDF with a real text
-- layer never gets one — it never needed OCR) — up to ~2M characters each
-- — on every sweep: after every upload, at startup, and every few minutes.
-- On a real library that's tens of seconds of solid CPU and disk reads
-- while holding the app's single database connection, so every other
-- request queued behind it.
--
-- The answer is known when a file is indexed (FinalizeUpload/
-- ReplaceContent/ReindexAllContent → ItemService.indexContent), so it's
-- recorded there instead: a row here means "needs OCR and hasn't had an
-- attempt yet". ListPending becomes a read of this small table.
CREATE TABLE ocr_queue (
    item_id   TEXT PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    queued_at INTEGER NOT NULL
);

-- One-time backfill: exactly what the old ListPending query would have
-- returned (minus its LIMIT), so nothing already waiting is dropped. This
-- is the same expensive scan the old query ran on every sweep, paid once
-- here at upgrade time instead of forever. Trashed items are included
-- (ListPending filters them out at read time) so a restore doesn't lose
-- its place.
INSERT INTO ocr_queue (item_id, queued_at)
SELECT items.id, items.created_at
FROM items
LEFT JOIN item_content_fts ON item_content_fts.item_id = items.id
WHERE items.type = 'file' AND items.target_id IS NULL
  AND (lower(items.name) LIKE '%.pdf'
       OR lower(items.name) LIKE '%.jpg'
       OR lower(items.name) LIKE '%.jpeg'
       OR lower(items.name) LIKE '%.png')
  AND NOT EXISTS (SELECT 1 FROM ocr_attempts WHERE ocr_attempts.item_id = items.id)
  AND (item_content_fts.content IS NULL
       OR trim(replace(item_content_fts.content, char(12), '')) = '');
