package panelapi

import (
	"context"
	"errors"
	"net/http"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

const orgContextKey contextKey = userContextKey + 1

// orgAccess is what requireOrgRole resolved for a request: which org, and the
// caller's effective role in it (a platform admin is treated as owner).
type orgAccess struct {
	Org  *paneldb.Org
	Role paneldb.Role
}

func withOrgAccess(ctx context.Context, a *orgAccess) context.Context {
	return context.WithValue(ctx, orgContextKey, a)
}

func orgAccessFromContext(ctx context.Context) *orgAccess {
	a, _ := ctx.Value(orgContextKey).(*orgAccess)
	return a
}

func (s *Server) resolveOrgAccess(ctx context.Context, user *paneldb.User, slug string, min paneldb.Role) (*orgAccess, int, string) {
	org, err := s.DB.GetOrgBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, paneldb.ErrNotFound) {
			return nil, http.StatusNotFound, "organization not found"
		}
		return nil, http.StatusInternalServerError, err.Error()
	}

	role := paneldb.RoleOwner // platform admin
	if !user.IsAdmin {
		role, err = s.DB.GetMembership(ctx, org.ID, user.ID)
		if err != nil {
			if errors.Is(err, paneldb.ErrNotFound) {
				return nil, http.StatusNotFound, "organization not found"
			}
			return nil, http.StatusInternalServerError, err.Error()
		}
	}
	if !role.AtLeast(min) {
		return nil, http.StatusForbidden, "insufficient role in this organization"
	}
	return &orgAccess{Org: org, Role: role}, 0, ""
}

func (s *Server) requireOrgRole(min paneldb.Role, next http.Handler) http.Handler {
	return s.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		acc, status, msg := s.resolveOrgAccess(r.Context(), userFromContext(r.Context()), r.PathValue("org"), min)
		if acc == nil {
			writeError(w, status, msg)
			return
		}
		r.SetPathValue("namespace", gameserversv1alpha1.TenantNamespace(acc.Org.Slug))
		next.ServeHTTP(w, r.WithContext(withOrgAccess(r.Context(), acc)))
	}))
}
