package paneldb

import (
	"context"
	"database/sql"
	"errors"
)

// SetDiscordLink links userID to a Discord account. ErrAlreadyExists when that Discord account is
// linked to another user. Linking again replaces the user's previous link.
func (s *Store) SetDiscordLink(ctx context.Context, userID int64, discordID, discordName string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET discord_user_id = $2, discord_username = $3, updated_at = now() WHERE id = $1`,
		userID, discordID, discordName)
	if isUniqueViolation(err) {
		return ErrAlreadyExists
	}
	return affectedOne(res, err)
}

// ClearDiscordLink removes userID's Discord link.
func (s *Store) ClearDiscordLink(ctx context.Context, userID int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET discord_user_id = NULL, discord_username = '', updated_at = now() WHERE id = $1`, userID)
	return affectedOne(res, err)
}

// GetUserByDiscordID returns the user linked to a Discord account, or ErrNotFound.
func (s *Store) GetUserByDiscordID(ctx context.Context, discordID string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns("")+` FROM users WHERE discord_user_id = $1`, discordID))
}

// SetOrgGuild connects orgID to a Discord guild, replacing its previous guild. ErrAlreadyExists
// when the guild is connected to another organization.
func (s *Store) SetOrgGuild(ctx context.Context, orgID int64, guildID, guildName string, linkedBy int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed
	var owner int64
	switch err := tx.QueryRowContext(ctx, `SELECT org_id FROM org_discord_guilds WHERE guild_id = $1`, guildID).Scan(&owner); {
	case err == nil && owner != orgID:
		return ErrAlreadyExists
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM org_discord_guilds WHERE org_id = $1`, orgID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO org_discord_guilds (org_id, guild_id, guild_name, linked_by) VALUES ($1, $2, $3, $4)`,
		orgID, guildID, guildName, linkedBy); err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return err
	}
	return tx.Commit()
}

// ClearOrgGuild disconnects orgID from its Discord guild (no error when it had none).
func (s *Store) ClearOrgGuild(ctx context.Context, orgID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM org_discord_guilds WHERE org_id = $1`, orgID)
	return err
}

// GetOrgGuild returns orgID's guild, or ErrNotFound.
func (s *Store) GetOrgGuild(ctx context.Context, orgID int64) (guildID, guildName string, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT guild_id, guild_name FROM org_discord_guilds WHERE org_id = $1`, orgID).
		Scan(&guildID, &guildName)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrNotFound
	}
	return guildID, guildName, err
}

// GetOrgByGuild returns the organization a guild is connected to, or ErrNotFound.
func (s *Store) GetOrgByGuild(ctx context.Context, guildID string) (*Org, error) {
	o := &Org{}
	err := s.db.QueryRowContext(ctx, `
		SELECT o.id, o.slug, o.name, o.created_at
		FROM org_discord_guilds g JOIN organizations o ON o.id = g.org_id WHERE g.guild_id = $1`, guildID).
		Scan(&o.ID, &o.Slug, &o.Name, &o.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return o, err
}

// GetSetting returns a platform setting, "" when unset.
func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM platform_settings WHERE key = $1`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// SetSetting stores a platform setting.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed
	res, err := tx.ExecContext(ctx, `UPDATE platform_settings SET value = $2 WHERE key = $1`, key, value)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO platform_settings (key, value) VALUES ($1, $2)`, key, value); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteSetting removes a platform setting.
func (s *Store) DeleteSetting(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM platform_settings WHERE key = $1`, key)
	return err
}
