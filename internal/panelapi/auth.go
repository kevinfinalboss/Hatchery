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
// provision infrastructure or manage other users rather than operate inside
// one organization.
func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return s.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !userFromContext(r.Context()).IsAdmin {
			writeError(w, http.StatusForbidden, "admin access required")
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func (s *Server) optionalUser(r *http.Request) *paneldb.User {
	token := headerToken(r)
	if token == "" {
		return nil
	}
	u, err := s.DB.ValidateSession(r.Context(), token)
	if err != nil {
		return nil
	}
	return u
}

func userFromContext(ctx context.Context) *paneldb.User {
	u, _ := ctx.Value(userContextKey).(*paneldb.User)
	return u
}

// requireGameServerAccess is now a safety net rather than the authorization
// check: authorization happened in requireOrgRole, which every org-scoped
// route goes through before reaching a handler. It only asserts that ran, so a
// route registered without it fails closed instead of silently open.
func (s *Server) requireGameServerAccess(w http.ResponseWriter, r *http.Request) bool {
	if orgAccessFromContext(r.Context()) == nil {
		writeError(w, http.StatusInternalServerError, "route is not org-scoped")
		return false
	}
	return true
}
