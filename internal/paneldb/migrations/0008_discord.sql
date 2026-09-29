-- Discord account link (set through OAuth2, never typed by the user). NULL = not linked.
ALTER TABLE users ADD COLUMN discord_user_id TEXT;
ALTER TABLE users ADD COLUMN discord_username TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX users_discord_user_id_key ON users (discord_user_id);

-- One Discord server (guild) per organization, and one organization per guild.
CREATE TABLE org_discord_guilds (
    org_id     BIGINT PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
    guild_id   TEXT NOT NULL UNIQUE,
    guild_name TEXT NOT NULL DEFAULT '',
    linked_by  BIGINT,
    linked_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Platform-wide settings (the platform's Discord guild, for now).
CREATE TABLE platform_settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
)
