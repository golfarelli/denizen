-- Full-text index of file content, for GET /api/v1/search — a standalone
-- FTS5 virtual table (not "external content", see its own doc comment
-- in internal/repository/search.go on why: at this app's scale, a plain
-- delete-then-reinsert on every re-index is simpler than keeping FTS5's
-- content= mode in sync with the items table). item_id is UNINDEXED —
-- stored and filterable, but not part of the full-text index itself, so
-- it never pollutes a content match.
--
-- unicode61 remove_diacritics=2 folds accents ("perché" matches a search
-- for "perche") — this app's real-world content is mostly Italian.
CREATE VIRTUAL TABLE item_content_fts USING fts5(
    item_id UNINDEXED,
    content,
    tokenize = 'unicode61 remove_diacritics 2'
);
