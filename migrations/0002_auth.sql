CREATE TABLE IF NOT EXISTS users (
    id         TEXT PRIMARY KEY,
    email      TEXT NOT NULL UNIQUE,
    nuid       TEXT NOT NULL,
    token      TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_users_token ON users(token);

-- Nullable: evaluations created before auth existed have no owner. Every
-- evaluation created going forward always sets this (enforced in the
-- application layer, not the schema, to avoid a destructive migration).
ALTER TABLE evaluations ADD COLUMN IF NOT EXISTS user_id TEXT REFERENCES users(id);

CREATE INDEX IF NOT EXISTS idx_evaluations_user_id ON evaluations(user_id);
