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
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Profile is the optional, self-edited part of an account. Validation of each
// field lives in the Panel API (internal/panelapi/profile.go); the store only
// persists.
type Profile struct {
	DisplayName       string
	Locale            string
	TimeZone          string
	Discord           string
	MinecraftUsername string
	SteamID           string
}

// User is a Panel account. Callers never see a password or its hash —
// passwords only ever cross the Store boundary as plaintext arguments,
// hashed with bcrypt immediately.
type User struct {
	ID        int64
	Username  string
	Email     string
	IsAdmin   bool
	CreatedAt time.Time
	Profile
}

// NormalizeEmail is the stored form of an e-mail: trimmed and lowercased, so
// the plain UNIQUE constraint is case-insensitive in practice.
func NormalizeEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// userColumns lists the users columns in scanUser's order, optionally prefixed
// with a table alias.
func userColumns(alias string) string {
	cols := []string{"id", "username", "email", "is_admin", "created_at",
		"display_name", "locale", "time_zone", "discord", "minecraft_username", "steam_id"}
	if alias != "" {
		for i, c := range cols {
			cols[i] = alias + "." + c
		}
	}
	return strings.Join(cols, ", ")
}

type rowScanner interface{ Scan(dest ...any) error }

func scanUserInto(row rowScanner, u *User) error {
	return row.Scan(&u.ID, &u.Username, &u.Email, &u.IsAdmin, &u.CreatedAt,
		&u.DisplayName, &u.Locale, &u.TimeZone, &u.Discord, &u.MinecraftUsername, &u.SteamID)
}

// scanUser scans one user, mapping "no rows" to ErrNotFound.
func scanUser(row rowScanner) (*User, error) {
	u := &User{}
	if err := scanUserInto(row, u); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return u, nil
}

func hashPassword(password string) ([]byte, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hashing password: %w", err)
	}
	return hash, nil
}

// userInsertError maps a unique violation on users to ErrEmailTaken or ErrAlreadyExists.
func userInsertError(err error) error {
	switch uniqueConstraint(err) {
	case "users_email_key":
		return ErrEmailTaken
	case "":
		return err
	default:
		return ErrAlreadyExists
	}
}

// CreateUser hashes password and inserts a new user. ErrEmailTaken if the
// e-mail is in use, ErrAlreadyExists if the username is.
func (s *Store) CreateUser(ctx context.Context, username, email, password string, isAdmin bool) (*User, error) {
	return createUser(ctx, s.db, username, email, password, isAdmin)
}

func createUser(ctx context.Context, q queryer, username, email, password string, isAdmin bool) (*User, error) {
	hash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}
	u, err := scanUser(q.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, is_admin) VALUES ($1, $2, $3, $4)
		RETURNING `+userColumns(""),
		username, NormalizeEmail(email), hash, isAdmin))
	if err != nil {
		return nil, userInsertError(err)
	}
	return u, nil
}

// UpsertUser creates username if it doesn't exist, or resets its password,
// e-mail and admin flag if it does. Used by the admin bootstrap (see
// internal/panelapi/bootstrap.go) so re-running it after a partial failure is
// idempotent instead of erroring on "already exists".
func (s *Store) UpsertUser(ctx context.Context, username, email, password string, isAdmin bool) (*User, error) {
	hash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}
	u, err := scanUser(s.db.QueryRowContext(ctx, `
		INSERT INTO users (username, email, password_hash, is_admin) VALUES ($1, $2, $3, $4)
		ON CONFLICT (username) DO UPDATE SET
			email = EXCLUDED.email,
			password_hash = EXCLUDED.password_hash,
			is_admin = EXCLUDED.is_admin,
			updated_at = now()
		RETURNING `+userColumns(""),
		username, NormalizeEmail(email), hash, isAdmin))
	if err != nil {
		return nil, userInsertError(err)
	}
	return u, nil
}

var (
	dummyHashOnce sync.Once
	dummyHash     []byte
)

// dummyPasswordHash is a valid bcrypt hash (same cost as real ones) compared
// against when the account does not exist, so that case costs the same as a
// wrong password.
func dummyPasswordHash() []byte {
	dummyHashOnce.Do(func() {
		dummyHash, _ = bcrypt.GenerateFromPassword([]byte("hatchery-timing-equalizer"), bcrypt.DefaultCost)
	})
	return dummyHash
}

// VerifyPassword looks the account up by login — an e-mail when it contains
// "@" (usernames cannot), a username otherwise — and checks password.
// ErrNotFound for both "no such account" and "wrong password", on purpose.
func (s *Store) VerifyPassword(ctx context.Context, login, password string) (*User, error) {
	where, arg := "username = $1", login
	if strings.Contains(login, "@") {
		where, arg = "email = $1", NormalizeEmail(login)
	}
	u := &User{}
	var hash string
	row := s.db.QueryRowContext(ctx, `SELECT `+userColumns("")+`, password_hash FROM users WHERE `+where, arg)
	if err := row.Scan(&u.ID, &u.Username, &u.Email, &u.IsAdmin, &u.CreatedAt,
		&u.DisplayName, &u.Locale, &u.TimeZone, &u.Discord, &u.MinecraftUsername, &u.SteamID, &hash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			_ = bcrypt.CompareHashAndPassword(dummyPasswordHash(), []byte(password))
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return nil, ErrNotFound
	}
	return u, nil
}

// VerifyUserPassword checks password against userID's hash; ErrNotFound when wrong.
func (s *Store) VerifyUserPassword(ctx context.Context, userID int64, password string) error {
	var hash string
	if err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&hash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return ErrNotFound
	}
	return nil
}

// GetUser looks up a user by id.
func (s *Store) GetUser(ctx context.Context, id int64) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns("")+` FROM users WHERE id = $1`, id))
}

// GetUserByUsername looks up a user by username, or returns ErrNotFound.
func (s *Store) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns("")+` FROM users WHERE username = $1`, username))
}

// GetUserByEmail looks up a user by e-mail (any case), or returns ErrNotFound.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	return getUserByEmail(ctx, s.db, email)
}

func getUserByEmail(ctx context.Context, q queryer, email string) (*User, error) {
	return scanUser(q.QueryRowContext(ctx, `SELECT `+userColumns("")+` FROM users WHERE email = $1`, NormalizeEmail(email)))
}

// ListUsers returns every user, ordered by username.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+userColumns("")+` FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		var u User
		if err := scanUserInto(rows, &u); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// UpdateProfile replaces userID's profile fields.
func (s *Store) UpdateProfile(ctx context.Context, userID int64, p Profile) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE users SET display_name = $2, locale = $3, time_zone = $4, discord = $5,
			minecraft_username = $6, steam_id = $7, updated_at = now()
		WHERE id = $1`,
		userID, p.DisplayName, p.Locale, p.TimeZone, p.Discord, p.MinecraftUsername, p.SteamID)
	return affectedOne(res, err)
}

// SetPassword replaces userID's password. Session revocation is the caller's call.
func (s *Store) SetPassword(ctx context.Context, userID int64, password string) error {
	return setPassword(ctx, s.db, userID, password)
}

func setPassword(ctx context.Context, q queryer, userID int64, password string) error {
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	res, err := q.ExecContext(ctx, `UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`, userID, hash)
	return affectedOne(res, err)
}

// SetEmail moves userID to email directly (no confirmation). Used only when
// the panel has no e-mail configured; ErrEmailTaken if in use.
func (s *Store) SetEmail(ctx context.Context, userID int64, email string) (*User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx,
		`UPDATE users SET email = $2, updated_at = now() WHERE id = $1 RETURNING `+userColumns(""), userID, NormalizeEmail(email)))
	if err != nil {
		return nil, userInsertError(err)
	}
	return u, nil
}

// SetAdmin changes userID's platform-admin flag, refusing with ErrLastAdmin to
// demote the only admin. Every admin row is locked so two concurrent demotions
// cannot both see "another admin exists".
func (s *Store) SetAdmin(ctx context.Context, userID int64, isAdmin bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	if !isAdmin {
		rows, err := tx.QueryContext(ctx, `SELECT id FROM users WHERE is_admin FOR UPDATE`)
		if err != nil {
			return err
		}
		var admins []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			admins = append(admins, id)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if len(admins) == 1 && admins[0] == userID {
			return ErrLastAdmin
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE users SET is_admin = $2, updated_at = now() WHERE id = $1`, userID, isAdmin)
	if err := affectedOne(res, err); err != nil {
		return err
	}
	return tx.Commit()
}

// affectedOne turns "no row updated" into ErrNotFound.
func affectedOne(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteUser removes a user (and, via ON DELETE CASCADE, their sessions and
// memberships). It refuses with ErrLastOwner if the user is the only owner of
// any org, since that would leave the org with nobody able to manage it.
func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	// Lock every org this user owns so a concurrent role change cannot slip
	// a second owner out from under the check below.
	if _, err := tx.ExecContext(ctx, `
		SELECT o.id FROM organizations o JOIN memberships m ON m.org_id = o.id
		WHERE m.user_id = $1 AND m.role = 'owner' FOR UPDATE OF o`, id); err != nil {
		return err
	}
	var soleOwner bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM memberships m
			WHERE m.user_id = $1 AND m.role = 'owner'
			AND (SELECT count(*) FROM memberships o WHERE o.org_id = m.org_id AND o.role = 'owner') = 1
		)`, id).Scan(&soleOwner); err != nil {
		return err
	}
	if soleOwner {
		return ErrLastOwner
	}

	res, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// HasAdmin reports whether any admin user exists — used to decide whether
// the bootstrap admin still needs creating.
func (s *Store) HasAdmin(ctx context.Context) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE is_admin)`).Scan(&exists)
	return exists, err
}
