-- Direct, per-user file shares — "condivisione con utenti specifici, come
-- fa Google Drive", view-only, distinct from the token-based `shares`
-- table (which grants access to whoever holds the link, not a specific
-- identity). Files only for now, not folders — see
-- ItemService.GetIncludingTrashed's own comment on why that's a
-- deliberate v1 scope cut, not an oversight.
CREATE TABLE user_shares (
    id             TEXT PRIMARY KEY,
    item_id        TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    owner_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    shared_with_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at     INTEGER NOT NULL,
    UNIQUE (item_id, shared_with_id)
);

CREATE INDEX idx_user_shares_item ON user_shares(item_id);
CREATE INDEX idx_user_shares_shared_with ON user_shares(shared_with_id);
