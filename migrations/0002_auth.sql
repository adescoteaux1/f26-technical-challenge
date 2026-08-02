CREATE TABLE IF NOT EXISTS users (
    id         TEXT PRIMARY KEY,
    email      TEXT NOT NULL UNIQUE,
    nuid       TEXT NOT NULL,
    token      TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_users_token ON users(token);

-- Nullable: expeditions created before auth existed have no owner. Every
-- expedition created going forward always sets this (enforced in the
-- application layer, not the schema, to avoid a destructive migration).
ALTER TABLE expeditions ADD COLUMN IF NOT EXISTS user_id TEXT REFERENCES users(id);

CREATE INDEX IF NOT EXISTS idx_expeditions_user_id ON expeditions(user_id);
