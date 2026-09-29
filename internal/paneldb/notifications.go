package paneldb

import (
	"context"
	"database/sql"
)

// NotificationSettings is an organization's alert configuration. An empty DiscordWebhookURL
// means Discord is off; Muted lists the events that are switched off.
type NotificationSettings struct {
	DiscordWebhookURL string
	Muted             []string
}

// GetNotificationSettings returns orgID's settings; an org that never saved any gets the
// defaults (no Discord, nothing muted).
func (s *Store) GetNotificationSettings(ctx context.Context, orgID int64) (NotificationSettings, error) {
	var out NotificationSettings
	var url sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT discord_webhook_url FROM org_notification_settings WHERE org_id = $1`, orgID).Scan(&url)
	if err != nil && err != sql.ErrNoRows {
		return out, err
	}
	out.DiscordWebhookURL = url.String
	rows, err := s.db.QueryContext(ctx, `SELECT event FROM org_notification_muted WHERE org_id = $1 ORDER BY event`, orgID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return out, err
		}
		out.Muted = append(out.Muted, e)
	}
	return out, rows.Err()
}

// SetDiscordWebhook stores orgID's Discord webhook URL; "" removes it.
func (s *Store) SetDiscordWebhook(ctx context.Context, orgID int64, url string) error {
	value := sql.NullString{String: url, Valid: url != ""}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed
	res, err := tx.ExecContext(ctx,
		`UPDATE org_notification_settings SET discord_webhook_url = $2, updated_at = now() WHERE org_id = $1`, orgID, value)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO org_notification_settings (org_id, discord_webhook_url) VALUES ($1, $2)`, orgID, value); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetMutedEvents replaces orgID's list of switched-off events.
func (s *Store) SetMutedEvents(ctx context.Context, orgID int64, events []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed
	if _, err := tx.ExecContext(ctx, `DELETE FROM org_notification_muted WHERE org_id = $1`, orgID); err != nil {
		return err
	}
	for _, e := range events {
		if _, err := tx.ExecContext(ctx, `INSERT INTO org_notification_muted (org_id, event) VALUES ($1, $2)`, orgID, e); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// AlertRecipient is who gets an organization's alert e-mails.
type AlertRecipient struct {
	Username string
	Email    string
	Locale   string
}

// ListAlertRecipients returns the owners and admins of orgID who did not switch alert e-mails
// off, ordered by username.
func (s *Store) ListAlertRecipients(ctx context.Context, orgID int64) ([]AlertRecipient, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.username, u.email, u.locale
		FROM memberships m JOIN users u ON u.id = m.user_id
		WHERE m.org_id = $1 AND m.role IN ('owner', 'admin') AND u.notify_email
		ORDER BY u.username`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AlertRecipient
	for rows.Next() {
		var r AlertRecipient
		if err := rows.Scan(&r.Username, &r.Email, &r.Locale); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetNotifyEmail turns userID's alert e-mails on or off.
func (s *Store) SetNotifyEmail(ctx context.Context, userID int64, on bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET notify_email = $2, updated_at = now() WHERE id = $1`, userID, on)
	return affectedOne(res, err)
}
