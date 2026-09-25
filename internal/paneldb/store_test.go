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
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db := newIsolatedTestDB(t)
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("running migrations: %v", err)
	}
	return NewStore(db)
}

func newIsolatedTestDB(t *testing.T) *sql.DB {
	t.Helper()
	baseDSN := os.Getenv("POSTGRES_TEST_DSN")
	if baseDSN == "" {
		t.Skip("POSTGRES_TEST_DSN not set, skipping paneldb integration tests")
	}

	ctx := context.Background()
	admin, err := Open(ctx, baseDSN)
	if err != nil {
		t.Fatalf("opening admin connection: %v", err)
	}
	defer admin.Close()

	dbName := fmt.Sprintf("paneldb_test_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+dbName); err != nil {
		t.Fatalf("creating test database %s: %v", dbName, err)
	}
	t.Cleanup(func() {
		cleanup, err := Open(context.Background(), baseDSN)
		if err != nil {
			return
		}
		defer cleanup.Close()
		_, _ = cleanup.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+dbName)
	})

	u, err := url.Parse(baseDSN)
	if err != nil {
		t.Fatalf("POSTGRES_TEST_DSN must be a postgres:// URL: %v", err)
	}
	u.Path = "/" + dbName

	db, err := Open(ctx, u.String())
	if err != nil {
		t.Fatalf("opening test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestCreateUserAndVerifyPassword(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, "alice", "alice@example.com", "correct-horse-battery-staple", false)
	if err != nil {
		t.Fatal(err)
	}
	if u.ID == 0 {
		t.Fatal("expected a non-zero id")
	}

	if _, err := s.CreateUser(ctx, "alice", "alice2@example.com", "whatever", false); err != ErrAlreadyExists {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}

	if _, err := s.VerifyPassword(ctx, "alice", "wrong-password"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for wrong password, got %v", err)
	}
	if _, err := s.VerifyPassword(ctx, "does-not-exist", "whatever"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for unknown user, got %v", err)
	}

	verified, err := s.VerifyPassword(ctx, "alice", "correct-horse-battery-staple")
	if err != nil {
		t.Fatal(err)
	}
	if verified.ID != u.ID {
		t.Fatalf("expected user id %d, got %d", u.ID, verified.ID)
	}
}

func TestUpsertUserIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first, err := s.UpsertUser(ctx, "admin", "admin@example.com", "password-one", true)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.UpsertUser(ctx, "admin", "admin@example.com", "password-two", true)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected the same user id across upserts, got %d and %d", first.ID, second.ID)
	}

	if _, err := s.VerifyPassword(ctx, "admin", "password-one"); err != ErrNotFound {
		t.Fatal("expected the old password to no longer work")
	}
	if _, err := s.VerifyPassword(ctx, "admin", "password-two"); err != nil {
		t.Fatalf("expected the new password to work: %v", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, "bob", "bob@example.com", "password", false)
	if err != nil {
		t.Fatal(err)
	}

	token, expiresAt, err := s.CreateSession(ctx, u.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || expiresAt.Before(time.Now()) {
		t.Fatal("expected a token and a future expiry")
	}

	got, err := s.ValidateSession(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != u.ID {
		t.Fatalf("expected user %d, got %d", u.ID, got.ID)
	}

	if err := s.RevokeSession(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ValidateSession(ctx, token); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound after revoke, got %v", err)
	}
}

func TestSessionExpiry(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, "carol", "carol@example.com", "password", false)
	if err != nil {
		t.Fatal(err)
	}

	token, _, err := s.CreateSession(ctx, u.ID, -time.Minute) // already expired
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ValidateSession(ctx, token); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for an expired session, got %v", err)
	}
}

func TestDeleteUserCascadesSessions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, "erin", "erin@example.com", "password", false)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := s.CreateSession(ctx, u.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteUser(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUser(ctx, u.ID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound deleting an already-deleted user, got %v", err)
	}

	if _, err := s.ValidateSession(ctx, token); err != ErrNotFound {
		t.Fatalf("expected the session to be gone too (cascade), got %v", err)
	}
	if _, err := s.GetUser(ctx, u.ID); err != ErrNotFound {
		t.Fatal("expected the user to be gone")
	}
}

func TestVerifyPasswordUnknownUserCostsAsMuchAsAWrongPassword(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.CreateUser(ctx, "real", "real@example.com", "correct-horse", false); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	_, _ = s.VerifyPassword(ctx, "no-such-user", "whatever")
	unknown := time.Since(start)

	// bcrypt at DefaultCost takes tens of milliseconds. Without the dummy
	// comparison the unknown-user path is a single indexed SELECT, well under 5ms.
	if unknown < 5*time.Millisecond {
		t.Fatalf("unknown-user login took %v: it skipped the bcrypt comparison, so response time reveals which usernames exist", unknown)
	}
}
