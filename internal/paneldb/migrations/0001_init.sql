-- Users, sessions and per-GameServer permission grants for the Panel API's
-- own auth (see AGENTS.md — this replaces the placeholder static bearer
-- token from M3). No UUID extension dependency on purpose: plain
-- auto-increment ids for users/grants, and session tokens are opaque random
-- values generated in Go, stored here only as a SHA-256 hash.

CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    is_admin      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ
);
CREATE INDEX sessions_user_id_idx ON sessions(user_id);

-- A row here grants user_id access to one GameServer (namespace +
-- gameserver_name — a string pair, not a foreign key: GameServers live in
-- the Kubernetes API, not this database). is_admin users on the users table
-- bypass this table entirely and can reach every GameServer.
CREATE TABLE gameserver_permissions (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    namespace       TEXT NOT NULL,
    gameserver_name TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, namespace, gameserver_name)
);
CREATE INDEX gameserver_permissions_user_id_idx ON gameserver_permissions(user_id);
