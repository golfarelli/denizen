-- Per-user "starred" items — a favorite is a relationship between a user
-- and an item, not a property of the item itself (an owned favorite_bool
-- column on `items` couldn't represent "I starred something someone else
-- owns and shared with me", which is explicitly in scope — see
-- ItemService.ListFavorites's own doc comment). Same shape as
-- 0002_user_shares.sql for the same reason.
CREATE TABLE item_favorites (
    id         TEXT PRIMARY KEY,
    item_id    TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at INTEGER NOT NULL,
    UNIQUE (item_id, user_id)
);

CREATE INDEX idx_item_favorites_user ON item_favorites(user_id);
