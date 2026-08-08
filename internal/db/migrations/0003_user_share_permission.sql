-- Adds a permission level to direct per-user shares (model.UserShare):
-- 'view' (the only option before this migration, so it's the default for
-- every existing row) or 'edit'. Also lifts the files-only restriction
-- documented on 0002_user_shares.sql — see ItemService.resolveGrant for
-- how a folder grant is now inherited by everything nested inside it.
ALTER TABLE user_shares ADD COLUMN permission TEXT NOT NULL DEFAULT 'view'
    CHECK (permission IN ('view', 'edit'));
