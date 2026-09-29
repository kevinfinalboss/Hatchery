package paneldb

import (
	"context"
	"database/sql"
	"errors"
)

// GetTOTP returns a user's encrypted TOTP secret and whether two-factor authentication is on.
func (s *Store) GetTOTP(ctx context.Context, userID int64) (secretEnc string, enabled bool, err error) {
	var secret sql.NullString
	err = s.db.QueryRowContext(ctx,
		`SELECT totp_secret, totp_enabled_at IS NOT NULL FROM users WHERE id = $1`, userID).Scan(&secret, &enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, ErrNotFound
	}
	return secret.String, enabled, err
}

// EnableTOTP turns two-factor authentication on with the given encrypted secret and recovery code
// hashes. step is the step of the code that confirmed the setup, so that code cannot log in again.
func (s *Store) EnableTOTP(ctx context.Context, userID int64, secretEnc string, step int64, codeHashes []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`UPDATE users SET totp_secret = $2, totp_enabled_at = now(), totp_last_step = $3 WHERE id = $1`,
		userID, secretEnc, step); err != nil {
		return err
	}
	if err := replaceRecoveryCodes(ctx, tx, userID, codeHashes); err != nil {
		return err
	}
	return tx.Commit()
}

// DisableTOTP turns two-factor authentication off and deletes the recovery codes.
func (s *Store) DisableTOTP(ctx context.Context, userID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`UPDATE users SET totp_secret = NULL, totp_enabled_at = NULL, totp_last_step = 0 WHERE id = $1`, userID); err != nil {
		return err
	}
	if err := replaceRecoveryCodes(ctx, tx, userID, nil); err != nil {
		return err
	}
	return tx.Commit()
}

// AcceptTOTPStep records step as used when it is newer than the last accepted one. false means the
// code was already used (or is older): a replay. The check and the write are one statement, so two
// concurrent logins with the same code cannot both pass.
func (s *Store) AcceptTOTPStep(ctx context.Context, userID, step int64) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET totp_last_step = $2 WHERE id = $1 AND totp_last_step < $2`, userID, step)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// ReplaceRecoveryCodes swaps a user's recovery codes for new ones.
func (s *Store) ReplaceRecoveryCodes(ctx context.Context, userID int64, hashes []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := replaceRecoveryCodes(ctx, tx, userID, hashes); err != nil {
		return err
	}
	return tx.Commit()
}

func replaceRecoveryCodes(ctx context.Context, tx *sql.Tx, userID int64, hashes []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_recovery_codes WHERE user_id = $1`, userID); err != nil {
		return err
	}
	for _, h := range hashes {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO user_recovery_codes (user_id, code_hash) VALUES ($1, $2)`, userID, h); err != nil {
			return err
		}
	}
	return nil
}

// UseRecoveryCode consumes a recovery code; false means it does not exist (or was already used).
func (s *Store) UseRecoveryCode(ctx context.Context, userID int64, hash string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM user_recovery_codes WHERE user_id = $1 AND code_hash = $2`, userID, hash)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// CountRecoveryCodes returns how many unused recovery codes the user has left.
func (s *Store) CountRecoveryCodes(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM user_recovery_codes WHERE user_id = $1`, userID).Scan(&n)
	return n, err
}

// SetOrgRequire2FA turns the organization's two-factor requirement on or off.
func (s *Store) SetOrgRequire2FA(ctx context.Context, orgID int64, on bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE organizations SET require_2fa = $2 WHERE id = $1`, orgID, on)
	return err
}

// CountMembersWithout2FA returns how many members of the org have not turned two-factor on.
func (s *Store) CountMembersWithout2FA(ctx context.Context, orgID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT count(*) FROM memberships m JOIN users u ON u.id = m.user_id
		WHERE m.org_id = $1 AND u.totp_enabled_at IS NULL`, orgID).Scan(&n)
	return n, err
}
