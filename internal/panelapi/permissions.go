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

package panelapi

import (
	"net/http"

	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

// Effective returns what the caller may do on server: everything for admin/owner (and the
// platform admin, who resolves as owner), the union of the '*' grant and the server's own grant
// for a member, nil when the member has no grant on it at all.
func (a *orgAccess) Effective(server string) []paneldb.Permission {
	if a.Role.AtLeast(paneldb.RoleAdmin) {
		return append([]paneldb.Permission{}, paneldb.AllPermissions...)
	}
	set := map[paneldb.Permission]bool{}
	found := false
	for _, g := range a.Grants {
		if g.GameServer == paneldb.AllServers || g.GameServer == server {
			found = true
			for _, p := range g.Permissions {
				set[p] = true
			}
		}
	}
	if !found {
		return nil
	}
	out := []paneldb.Permission{}
	for _, p := range paneldb.AllPermissions {
		if set[p] {
			out = append(out, p)
		}
	}
	return out
}

func (a *orgAccess) Sees(server string) bool { return a.Effective(server) != nil }

func (a *orgAccess) Can(server string, p paneldb.Permission) bool {
	for _, q := range a.Effective(server) {
		if q == p {
			return true
		}
	}
	return false
}

// requireServerPermission gates a route on one GameServer ({name}). It must run inside
// requireOrgRole. A member with no grant on the server gets 404 — the server's existence is not
// revealed — and one with some grant but not p gets 403. p == "" only requires seeing the server.
func (s *Server) requireServerPermission(p paneldb.Permission, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		acc := orgAccessFromContext(r.Context())
		server := r.PathValue("name")
		if !acc.Sees(server) {
			writeError(w, http.StatusNotFound, "game server not found")
			return
		}
		if p != "" && !acc.Can(server, p) {
			writeError(w, http.StatusForbidden, "you do not have the "+string(p)+" permission on this server")
			return
		}
		next.ServeHTTP(w, r)
	})
}
