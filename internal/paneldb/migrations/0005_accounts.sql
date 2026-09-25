ALTER TABLE users ADD COLUMN email TEXT NOT NULL;
ALTER TABLE users ADD CONSTRAINT users_email_key UNIQUE (email);
ALTER TABLE users ADD COLUMN display_name TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN locale TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN time_zone TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN discord TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN minecraft_username TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN steam_id TEXT NOT NULL DEFAULT '';

-- One pending invitation per (org, e-mail): inviting again replaces it.
-- Accepting or revoking deletes the row.
CREATE TABLE invitations (
    id          BIGSERIAL PRIMARY KEY,
    org_id      BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    email       TEXT NOT NULL,
    role        TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'member')),
    token_hash  TEXT NOT NULL UNIQUE,
    locale      TEXT NOT NULL DEFAULT '',
    invited_by  BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL,
    UNIQUE (org_id, email)
);

-- Single-use links for people who already have an account.
CREATE TABLE user_tokens (
    token_hash  TEXT PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    purpose     TEXT NOT NULL CHECK (purpose IN ('password_reset', 'email_change')),
    new_email   TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ
);
CREATE INDEX user_tokens_user_id_idx ON user_tokens(user_id)
