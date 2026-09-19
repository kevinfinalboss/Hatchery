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
	"time"

	"golang.org/x/crypto/bcrypt"
)

// User is a Panel account. Callers never see a password or its hash —
// passwords only ever cross the Store boundary as plaintext arguments to
// CreateUser/UpsertUser/VerifyPassword, hashed with bcrypt immediately.
type User struct {
	ID        int64
	Username  string
	IsAdmin   bool
	CreatedAt time.Time
}

// CreateUser hashes password and inserts a new user. Returns ErrAlreadyExists
// if username is taken.
func (s *Store) CreateUser(ctx context.Context, username, password string, isAdmin bool) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hashing password: %w", err)
	}

	u := &User{Username: username, IsAdmin: isAdmin}
	row := s.db.QueryRowContext(ctx,
		`INSERT INTO users (username, password_hash, is_admin) VALUES ($1, $2, $3) RETURNING id, created_at`,
		username, hash, isAdmin)
	if err := row.Scan(&u.ID, &u.CreatedAt); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrAlreadyExists
		}
		return nil, err
	}
	return u, nil
}

// UpsertUser creates username if it doesn't exist, or resets its password
// and admin flag if it does. Used by the admin bootstrap (see
// internal/panelapi/bootstrap.go) so re-running it after a partial failure
// — the process crashing between writing the user and writing the
// credentials Secret — is idempotent instead of erroring on "already
// exists".
func (s *Store) UpsertUser(ctx context.Context, username, password string, isAdmin bool) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hashing password: %w", err)
	}

	u := &User{Username: username, IsAdmin: isAdmin}
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO users (username, password_hash, is_admin) VALUES ($1, $2, $3)
		ON CONFLICT (username) DO UPDATE SET
			password_hash = EXCLUDED.password_hash,
			is_admin = EXCLUDED.is_admin,
			updated_at = now()
		RETURNING id, created_at`,
		username, hash, isAdmin)
	if err := row.Scan(&u.ID, &u.CreatedAt); err != nil {
		return nil, err
	}
	return u, nil
}

// VerifyPassword looks up username and checks password against its stored
// hash. Returns ErrNotFound for both "no such user" and "wrong password" —
// deliberately the same error, so a caller can't use response differences to
// enumerate valid usernames.
func (s *Store) VerifyPassword(ctx context.Context, username, password string) (*User, error) {
	u := &User{}
	var hash string
	row := s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, is_admin, created_at FROM users WHERE username = $1`, username)
	if err := row.Scan(&u.ID, &u.Username, &hash, &u.IsAdmin, &u.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return nil, ErrNotFound
	}
	return u, nil
}

// GetUser looks up a user by id.
func (s *Store) GetUser(ctx context.Context, id int64) (*User, error) {
	u := &User{}
	row := s.db.QueryRowContext(ctx, `SELECT id, username, is_admin, created_at FROM users WHERE id = $1`, id)
	if err := row.Scan(&u.ID, &u.Username, &u.IsAdmin, &u.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return u, nil
}

// ListUsers returns every user, ordered by username.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, username, is_admin, created_at FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.IsAdmin, &u.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// DeleteUser removes a user (and, via ON DELETE CASCADE, their sessions and
// GameServer permission grants).
func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, id)
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

// HasAdmin reports whether any admin user exists — used to decide whether
// the bootstrap admin still needs creating.
func (s *Store) HasAdmin(ctx context.Context) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE is_admin)`).Scan(&exists)
	return exists, err
}
