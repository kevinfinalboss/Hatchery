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
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// ErrNotFound is returned when a lookup (user, session, ...) finds nothing.
var ErrNotFound = errors.New("not found")

// ErrAlreadyExists is returned when a create would violate a uniqueness
// constraint (e.g. a username that's already taken).
var ErrAlreadyExists = errors.New("already exists")

// Store is the Panel API's handle to its Postgres database.
type Store struct {
	db *sql.DB
}

// NewStore wraps an already-open, already-migrated *sql.DB.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

const uniqueViolationCode = "23505"

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode
}
