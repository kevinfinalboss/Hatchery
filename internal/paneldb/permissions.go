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
	"sort"
)

// Permission is one thing an org member with role 'member' may do on a GameServer.
type Permission string

const (
	PermConsoleRead   Permission = "console.read"
	PermConsoleWrite  Permission = "console.write"
	PermPower         Permission = "power"
	PermFilesRead     Permission = "files.read"
	PermFilesWrite    Permission = "files.write"
	PermBackupsRead   Permission = "backups.read"
	PermBackupsManage Permission = "backups.manage"
	PermSchedules     Permission = "schedules"
)

// AllPermissions lists every grantable permission in display order.
var AllPermissions = []Permission{
	PermConsoleRead, PermConsoleWrite, PermPower, PermFilesRead, PermFilesWrite,
	PermBackupsRead, PermBackupsManage, PermSchedules,
}

// AllServers as a Grant's GameServer means every server of the org, including later ones.
const AllServers = "*"

// Valid reports whether p is one of AllPermissions.
func (p Permission) Valid() bool {
	for _, q := range AllPermissions {
		if p == q {
			return true
		}
	}
	return false
}

func permOrder(p Permission) int {
	for i, q := range AllPermissions {
		if p == q {
			return i
		}
	}
	return len(AllPermissions)
}

// Grant is a member's permissions on one server (or on AllServers).
type Grant struct {
	GameServer  string       `json:"gameserver"`
	Permissions []Permission `json:"permissions"`
}

// ListMemberGrants returns a member's grants, AllServers first, then by server name.
func (s *Store) ListMemberGrants(ctx context.Context, orgID, userID int64) ([]Grant, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT gameserver, permission FROM member_permissions WHERE org_id = $1 AND user_id = $2`, orgID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byServer := map[string][]Permission{}
	for rows.Next() {
		var gs string
		var p Permission
		if err := rows.Scan(&gs, &p); err != nil {
			return nil, err
		}
		byServer[gs] = append(byServer[gs], p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]Grant, 0, len(byServer))
	for gs, perms := range byServer {
		sort.Slice(perms, func(i, j int) bool { return permOrder(perms[i]) < permOrder(perms[j]) })
		out = append(out, Grant{GameServer: gs, Permissions: perms})
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].GameServer == AllServers) != (out[j].GameServer == AllServers) {
			return out[i].GameServer == AllServers
		}
		return out[i].GameServer < out[j].GameServer
	})
	return out, nil
}

// SetMemberGrants replaces all of a member's grants. Duplicates are dropped; callers validate
// permission names and server existence first.
func (s *Store) SetMemberGrants(ctx context.Context, orgID, userID int64, grants []Grant) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed
	if _, err := tx.ExecContext(ctx, `DELETE FROM member_permissions WHERE org_id = $1 AND user_id = $2`, orgID, userID); err != nil {
		return err
	}
	seen := map[[2]string]bool{}
	for _, g := range grants {
		for _, p := range g.Permissions {
			key := [2]string{g.GameServer, string(p)}
			if seen[key] {
				continue
			}
			seen[key] = true
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO member_permissions (org_id, user_id, gameserver, permission) VALUES ($1, $2, $3, $4)`,
				orgID, userID, g.GameServer, string(p)); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// DeleteGameServerGrants drops every member's grants on one server (called when it is deleted, so
// a new server with the same name does not inherit them). AllServers grants are untouched.
func (s *Store) DeleteGameServerGrants(ctx context.Context, orgID int64, gameserver string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM member_permissions WHERE org_id = $1 AND gameserver = $2`, orgID, gameserver)
	return err
}
