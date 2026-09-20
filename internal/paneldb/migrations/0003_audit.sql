CREATE TABLE audit_events (
    id             BIGSERIAL PRIMARY KEY,
    org_slug       TEXT,
    actor_user_id  BIGINT,
    actor_username TEXT NOT NULL,
    action         TEXT NOT NULL,
    target_type    TEXT NOT NULL DEFAULT '',
    target_name    TEXT NOT NULL DEFAULT '',
    outcome        TEXT NOT NULL,
    ip             TEXT NOT NULL DEFAULT '',
    metadata       TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX audit_events_org_idx ON audit_events (org_slug, id DESC);
