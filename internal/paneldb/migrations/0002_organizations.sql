DROP TABLE gameserver_permissions;

CREATE TABLE organizations (
    id         BIGSERIAL PRIMARY KEY,
    slug       TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A user can belong to many orgs, once each. The at-least-one-owner rule is
-- enforced in Go inside a transaction that locks the organization row.
CREATE TABLE memberships (
    org_id     BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'member')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, user_id)
);
CREATE INDEX memberships_user_id_idx ON memberships(user_id);
