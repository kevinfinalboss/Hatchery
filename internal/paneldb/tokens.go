/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package paneldb

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// TokenPurpose says what a single-use user token may be spent on.
type TokenPurpose string

const (
	PurposePasswordReset TokenPurpose = "password_reset"
	PurposeEmailChange   TokenPurpose = "email_change"
)

// ErrInvalidToken covers every way a link can be unusable (unknown, used,
// expired, wrong purpose): the caller shows one message for all of them.
var ErrInvalidToken = errors.New("invalid or expired link")

// IssueUserToken mints a single-use token for userID. Older unused tokens of
// the same purpose are deleted first, so only the latest e-mailed link works.
func (s *Store) IssueUserToken(ctx context.Context, userID int64, purpose TokenPurpose, newEmail string, ttl time.Duration) (string, error) {
	raw, hash, err := newToken()
	if err != nil {
		return "", err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM user_tokens WHERE user_id = $1 AND purpose = $2 AND used_at IS NULL`, userID, string(purpose)); err != nil {
		return "", err
	}
	var email sql.NullString
	if newEmail != "" {
		email = sql.NullString{String: NormalizeEmail(newEmail), Valid: true}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO user_tokens (token_hash, user_id, purpose, new_email, expires_at) VALUES ($1, $2, $3, $4, $5)`,
		hash, userID, string(purpose), email, time.Now().Add(ttl)); err != nil {
		return "", err
	}
	return raw, tx.Commit()
}

// consumeUserToken marks token used and returns its user and new_email. The
// single UPDATE ... WHERE used_at IS NULL makes concurrent spends race-free:
// only one of them gets a row back.
func consumeUserToken(ctx context.Context, q queryer, token string, purpose TokenPurpose) (int64, string, error) {
	var userID int64
	var newEmail sql.NullString
	err := q.QueryRowContext(ctx, `
		UPDATE user_tokens SET used_at = now()
		WHERE token_hash = $1 AND purpose = $2 AND used_at IS NULL AND expires_at > now()
		RETURNING user_id, new_email`, hashToken(token), string(purpose)).Scan(&userID, &newEmail)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", ErrInvalidToken
	}
	return userID, newEmail.String, err
}

// ResetPassword spends a password_reset token: sets the new password and
// revokes every session of the account, in one transaction.
func (s *Store) ResetPassword(ctx context.Context, token, newPassword string) (*User, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed
	userID, _, err := consumeUserToken(ctx, tx, token, PurposePasswordReset)
	if err != nil {
		return nil, err
	}
	if err := setPassword(ctx, tx, userID, newPassword); err != nil {
		return nil, err
	}
	if err := revokeUserSessions(ctx, tx, userID, ""); err != nil {
		return nil, err
	}
	u, err := scanUser(tx.QueryRowContext(ctx, `SELECT `+userColumns("")+` FROM users WHERE id = $1`, userID))
	if err != nil {
		return nil, err
	}
	return u, tx.Commit()
}

// ConfirmEmailChange spends an email_change token and moves the account to the
// new address. ErrEmailTaken if another account took it meanwhile (the token
// stays unspent: the transaction rolls back).
func (s *Store) ConfirmEmailChange(ctx context.Context, token string) (*User, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed
	userID, newEmail, err := consumeUserToken(ctx, tx, token, PurposeEmailChange)
	if err != nil {
		return nil, err
	}
	u, err := scanUser(tx.QueryRowContext(ctx,
		`UPDATE users SET email = $2, updated_at = now() WHERE id = $1 RETURNING `+userColumns(""), userID, newEmail))
	if err != nil {
		return nil, userInsertError(err)
	}
	return u, tx.Commit()
}
