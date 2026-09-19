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

// Package paneldb is the Panel API's own datastore: users, login sessions
// and per-GameServer permission grants. It's deliberately not another CRD —
// see AGENTS.md's note on why this kind of data (relational, high-churn,
// unrelated to cluster state) belongs in a real database instead of etcd.
package paneldb

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"path"
	"sort"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Open opens a connection pool to Postgres at dsn and verifies connectivity
// before returning.
func Open(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connecting to database: %w", err)
	}
	return db, nil
}

// Migrate applies every migration under migrations/ not yet recorded in
// schema_migrations, in filename order, each in its own transaction. Safe to
// call on every startup — already-applied migrations are skipped.
//
// Statements within one migration file are split on ";" and executed one at
// a time rather than as a single multi-statement Exec: pgx's default
// extended query protocol doesn't support multiple statements per Exec call,
// and reconfiguring the connection for simple-protocol mode just to allow
// that felt like a bigger knob to turn than "don't put semicolons inside
// string literals in migration files," which is an easy rule to follow for
// schema files this small.
func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("creating schema_migrations: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("reading embedded migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		var applied bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`, name).Scan(&applied); err != nil {
			return fmt.Errorf("checking migration %s: %w", name, err)
		}
		if applied {
			continue
		}
		if err := applyMigration(ctx, db, name); err != nil {
			return err
		}
	}
	return nil
}

func applyMigration(ctx context.Context, db *sql.DB, name string) error {
	sqlBytes, err := migrationsFS.ReadFile(path.Join("migrations", name))
	if err != nil {
		return fmt.Errorf("reading migration %s: %w", name, err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction for %s: %w", name, err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	for _, stmt := range splitStatements(string(sqlBytes)) {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("applying migration %s: %w", name, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, name); err != nil {
		return fmt.Errorf("recording migration %s: %w", name, err)
	}
	return tx.Commit()
}

func splitStatements(sqlText string) []string {
	raw := strings.Split(sqlText, ";")
	stmts := make([]string, 0, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(s)
		// Note: a leading "--" is not "this chunk is just a comment, skip
		// it" — a statement preceded by comment lines (as every statement in
		// these migration files is) starts with "--" too, and Postgres
		// already strips "--" line comments on its own when executing a
		// statement. Only genuinely empty chunks (a trailing blank line
		// after the last ";") get dropped here.
		if s == "" {
			continue
		}
		stmts = append(stmts, s)
	}
	return stmts
}
