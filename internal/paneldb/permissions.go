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

import "context"

// GameServerRef identifies a GameServer by the same coordinates Kubernetes
// does — namespace and name — since GameServers live in the Kubernetes API,
// not this database, and there's nothing to foreign-key against here.
type GameServerRef struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// GrantGameServerAccess gives userID access to one GameServer. A repeat
// grant is a no-op, not an error.
func (s *Store) GrantGameServerAccess(ctx context.Context, userID int64, ref GameServerRef) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO gameserver_permissions (user_id, namespace, gameserver_name)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, namespace, gameserver_name) DO NOTHING`,
		userID, ref.Namespace, ref.Name)
	return err
}

// RevokeGameServerAccess removes a grant, if any. Revoking a grant that
// doesn't exist is a no-op, not an error.
func (s *Store) RevokeGameServerAccess(ctx context.Context, userID int64, ref GameServerRef) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM gameserver_permissions WHERE user_id = $1 AND namespace = $2 AND gameserver_name = $3`,
		userID, ref.Namespace, ref.Name)
	return err
}

// HasGameServerAccess reports whether userID has an explicit grant for ref.
// It never consults User.IsAdmin — callers that want "admins can reach
// everything" semantics check that separately (see
// internal/panelapi/authz.go), so this stays a pure permissions-table
// lookup.
func (s *Store) HasGameServerAccess(ctx context.Context, userID int64, ref GameServerRef) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM gameserver_permissions WHERE user_id = $1 AND namespace = $2 AND gameserver_name = $3)`,
		userID, ref.Namespace, ref.Name).Scan(&exists)
	return exists, err
}

// ListGameServerAccess returns every GameServer userID has an explicit grant
// for (meaningless/incomplete for an admin user, who can reach everything
// regardless of this table).
func (s *Store) ListGameServerAccess(ctx context.Context, userID int64) ([]GameServerRef, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT namespace, gameserver_name FROM gameserver_permissions WHERE user_id = $1 ORDER BY namespace, gameserver_name`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []GameServerRef
	for rows.Next() {
		var ref GameServerRef
		if err := rows.Scan(&ref.Namespace, &ref.Name); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}
