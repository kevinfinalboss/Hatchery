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

// ErrWrongAccount is returned when an invitation is accepted by an account
// whose e-mail is not the invited one.
var ErrWrongAccount = errors.New("invitation is for another account")

// Invitation is a pending invitation to join an org.
type Invitation struct {
	ID        int64     `json:"id"`
	OrgID     int64     `json:"-"`
	OrgSlug   string    `json:"-"`
	OrgName   string    `json:"-"`
	Email     string    `json:"email"`
	Role      Role      `json:"role"`
	Locale    string    `json:"-"`
	InvitedBy string    `json:"invitedBy"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	Expired   bool      `json:"expired"`
}

const invitationSelect = `
	SELECT i.id, i.org_id, o.slug, o.name, i.email, i.role, i.locale, COALESCE(u.username, ''),
		i.created_at, i.expires_at, i.expires_at <= now()
	FROM invitations i
	JOIN organizations o ON o.id = i.org_id
	LEFT JOIN users u ON u.id = i.invited_by`

func scanInvitation(row rowScanner) (*Invitation, error) {
	inv := &Invitation{}
	err := row.Scan(&inv.ID, &inv.OrgID, &inv.OrgSlug, &inv.OrgName, &inv.Email, &inv.Role, &inv.Locale,
		&inv.InvitedBy, &inv.CreatedAt, &inv.ExpiresAt, &inv.Expired)
	return inv, err
}

// CreateInvitation creates, or replaces, the invitation of email to orgID and
// returns it with its raw token. Replacing gives a new token and validity, so
// the previous link stops working.
func (s *Store) CreateInvitation(ctx context.Context, orgID int64, email string, role Role, locale string, invitedBy int64, ttl time.Duration) (*Invitation, string, error) {
	raw, hash, err := newToken()
	if err != nil {
		return nil, "", err
	}
	var id int64
	if err := s.db.QueryRowContext(ctx, `
		INSERT INTO invitations (org_id, email, role, token_hash, locale, invited_by, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (org_id, email) DO UPDATE SET
			role = EXCLUDED.role, token_hash = EXCLUDED.token_hash, locale = EXCLUDED.locale,
			invited_by = EXCLUDED.invited_by, created_at = now(), expires_at = EXCLUDED.expires_at
		RETURNING id`,
		orgID, NormalizeEmail(email), string(role), hash, locale, invitedBy, time.Now().Add(ttl)).Scan(&id); err != nil {
		return nil, "", err
	}
	inv, err := scanInvitation(s.db.QueryRowContext(ctx, invitationSelect+` WHERE i.id = $1`, id))
	return inv, raw, err
}

// ReissueInvitation gives an existing invitation a new token and validity.
func (s *Store) ReissueInvitation(ctx context.Context, orgID, id int64, ttl time.Duration) (*Invitation, string, error) {
	raw, hash, err := newToken()
	if err != nil {
		return nil, "", err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE invitations SET token_hash = $3, created_at = now(), expires_at = $4 WHERE org_id = $1 AND id = $2`,
		orgID, id, hash, time.Now().Add(ttl))
	if err := affectedOne(res, err); err != nil {
		return nil, "", err
	}
	inv, err := scanInvitation(s.db.QueryRowContext(ctx, invitationSelect+` WHERE i.id = $1`, id))
	return inv, raw, err
}

// ListInvitations returns orgID's invitations, newest first, including expired
// ones (the UI shows them as such). Invitations expired for over 30 days are
// pruned on the way.
func (s *Store) ListInvitations(ctx context.Context, orgID int64) ([]Invitation, error) {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM invitations WHERE org_id = $1 AND expires_at < now() - interval '30 days'`, orgID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, invitationSelect+` WHERE i.org_id = $1 ORDER BY i.created_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invitation
	for rows.Next() {
		inv, err := scanInvitation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *inv)
	}
	return out, rows.Err()
}

// DeleteInvitation revokes an invitation; ErrNotFound if it is not orgID's.
func (s *Store) DeleteInvitation(ctx context.Context, orgID, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM invitations WHERE org_id = $1 AND id = $2`, orgID, id)
	return affectedOne(res, err)
}

// GetInvitationByToken returns the live invitation for token, or ErrInvalidToken.
func (s *Store) GetInvitationByToken(ctx context.Context, token string) (*Invitation, error) {
	inv, err := scanInvitation(s.db.QueryRowContext(ctx,
		invitationSelect+` WHERE i.token_hash = $1 AND i.expires_at > now()`, hashToken(token)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvalidToken
	}
	return inv, err
}

// takeInvitation locks and deletes the live invitation for token inside tx.
// Two concurrent accepts serialize on the row: the second finds nothing.
func takeInvitation(ctx context.Context, tx *sql.Tx, token string) (*Invitation, error) {
	inv, err := scanInvitation(tx.QueryRowContext(ctx,
		invitationSelect+` WHERE i.token_hash = $1 AND i.expires_at > now() FOR UPDATE OF i`, hashToken(token)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvalidToken
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM invitations WHERE id = $1`, inv.ID); err != nil {
		return nil, err
	}
	return inv, nil
}

// AcceptInvitationNewUser creates the account for the invited e-mail (opening
// the link proves the person owns it) and its membership, spending the
// invitation. ErrAlreadyExists if the username is taken, ErrEmailTaken if an
// account with that e-mail appeared meanwhile.
func (s *Store) AcceptInvitationNewUser(ctx context.Context, token, username, password, displayName string) (*User, *Invitation, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed
	inv, err := takeInvitation(ctx, tx, token)
	if err != nil {
		return nil, nil, err
	}
	u, err := createUser(ctx, tx, username, inv.Email, password, false)
	if err != nil {
		return nil, nil, err
	}
	if displayName != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET display_name = $2 WHERE id = $1`, u.ID, displayName); err != nil {
			return nil, nil, err
		}
		u.DisplayName = displayName
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO memberships (org_id, user_id, role) VALUES ($1, $2, $3)`, inv.OrgID, u.ID, string(inv.Role)); err != nil {
		return nil, nil, err
	}
	return u, inv, tx.Commit()
}

// AcceptInvitationExistingUser adds userID to the invited org, if the
// invitation is for userID's e-mail. Already being a member just spends it.
func (s *Store) AcceptInvitationExistingUser(ctx context.Context, token string, userID int64) (*Invitation, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed
	inv, err := takeInvitation(ctx, tx, token)
	if err != nil {
		return nil, err
	}
	var email string
	if err := tx.QueryRowContext(ctx, `SELECT email FROM users WHERE id = $1`, userID).Scan(&email); err != nil {
		return nil, err
	}
	if email != inv.Email {
		return nil, ErrWrongAccount
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO memberships (org_id, user_id, role) VALUES ($1, $2, $3)
		ON CONFLICT (org_id, user_id) DO NOTHING`, inv.OrgID, userID, string(inv.Role)); err != nil {
		return nil, err
	}
	return inv, tx.Commit()
}
