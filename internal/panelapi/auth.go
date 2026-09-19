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
	"context"
	"net/http"
	"strings"

	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

type contextKey int

const userContextKey contextKey = iota

// requireAuth resolves the bearer token in the Authorization header to a
// user via paneldb (see paneldb.ValidateSession — a database lookup, not a
// signature check; there's no stateless option here since revocation needs
// the lookup anyway) and attaches it to the request context. Handlers read
// it back with userFromContext.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return s.authenticate(next, headerToken)
}

// requireAuthWS is requireAuth for the one route that can't use it: a
// browser's native WebSocket constructor cannot set an Authorization header
// on the upgrade request, so the console route also accepts the session
// token as a "token" query parameter. Every other route stays header-only —
// query strings end up in server access logs and browser history, which is
// fine for a value that's already meant to be sent over the wire on every
// request, but not worth widening beyond the one route that has no other
// option.
func (s *Server) requireAuthWS(next http.Handler) http.Handler {
	return s.authenticate(next, func(r *http.Request) string {
		if t := headerToken(r); t != "" {
			return t
		}
		return r.URL.Query().Get("token")
	})
}

func headerToken(r *http.Request) string {
	parts := strings.SplitN(r.Header.Get("Authorization"), " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return ""
	}
	return parts[1]
}

func (s *Server) authenticate(next http.Handler, extractToken func(*http.Request) string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractToken(r)
		if token == "" {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}

		user, err := s.DB.ValidateSession(r.Context(), token)
		if err != nil {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, "invalid or expired session")
			return
		}

		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireAdmin is requireAuth plus an is_admin check, for routes that
// provision infrastructure or manage other users rather than operate on a
// GameServer a permission grant could scope.
func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return s.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !userFromContext(r.Context()).IsAdmin {
			writeError(w, http.StatusForbidden, "admin access required")
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func userFromContext(ctx context.Context) *paneldb.User {
	u, _ := ctx.Value(userContextKey).(*paneldb.User)
	return u
}

// requireGameServerAccess reports whether the request's authenticated user
// may act on the GameServer named by the request's {namespace}/{name} path
// values: true for an admin, or for a user with an explicit grant. It writes
// the 403 response itself on denial, so callers just need to return when it
// reports false.
func (s *Server) requireGameServerAccess(w http.ResponseWriter, r *http.Request) bool {
	user := userFromContext(r.Context())
	if user.IsAdmin {
		return true
	}

	ref := paneldb.GameServerRef{Namespace: r.PathValue("namespace"), Name: r.PathValue("name")}
	has, err := s.DB.HasGameServerAccess(r.Context(), user.ID, ref)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return false
	}
	if !has {
		writeError(w, http.StatusForbidden, "you do not have access to this gameserver")
		return false
	}
	return true
}
