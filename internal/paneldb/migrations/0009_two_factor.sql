-- TOTP two-factor authentication. totp_secret is the secret encrypted with the panel key (NULL = no 2FA),
-- totp_enabled_at NULL = off, and totp_last_step is the last accepted 30-second step (a code of that step
-- or an earlier one is refused, so a code works once).
ALTER TABLE users ADD COLUMN totp_secret TEXT;
ALTER TABLE users ADD COLUMN totp_enabled_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN totp_last_step BIGINT NOT NULL DEFAULT 0;

-- Single-use recovery codes, SHA-256 only.
CREATE TABLE user_recovery_codes (
    user_id   BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash TEXT NOT NULL,
    PRIMARY KEY (user_id, code_hash)
);

-- An organization may require two-factor authentication from its members.
ALTER TABLE organizations ADD COLUMN require_2fa BOOLEAN NOT NULL DEFAULT FALSE
