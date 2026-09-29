-- Notification settings per organization. No row means Discord off and every event on.
CREATE TABLE org_notification_settings (
    org_id              BIGINT PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
    discord_webhook_url TEXT,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A row mutes one event for the organization: events added later start enabled.
CREATE TABLE org_notification_muted (
    org_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    event  TEXT NOT NULL,
    PRIMARY KEY (org_id, event)
);

ALTER TABLE users ADD COLUMN notify_email BOOLEAN NOT NULL DEFAULT TRUE;
