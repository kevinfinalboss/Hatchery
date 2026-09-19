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
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"
)

// Sessions are opaque random tokens, not JWTs: unlike the sftp-agent's
// per-GameServer tokens (pkg/authtoken), validating a Panel session already
// requires a database round trip either way — to check revocation — so a
// signature buys nothing here. Only the SHA-256 hash of a token is stored,
// so a database leak alone doesn't hand out usable sessions.

// CreateSession mints a new session for userID, valid for ttl, and returns
// the raw token to hand back to the client.
func (s *Store) CreateSession(ctx context.Context, userID int64, ttl time.Duration) (token string, expiresAt time.Time, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	expiresAt = time.Now().Add(ttl)

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`,
		hashToken(token), userID, expiresAt)
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

// ValidateSession returns the user for token if it exists, isn't revoked,
// and hasn't expired.
func (s *Store) ValidateSession(ctx context.Context, token string) (*User, error) {
	u := &User{}
	row := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.is_admin, u.created_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.revoked_at IS NULL AND s.expires_at > now()`,
		hashToken(token))
	if err := row.Scan(&u.ID, &u.Username, &u.IsAdmin, &u.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return u, nil
}

// RevokeSession invalidates token immediately (logout).
func (s *Store) RevokeSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET revoked_at = now() WHERE token_hash = $1 AND revoked_at IS NULL`,
		hashToken(token))
	return err
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
