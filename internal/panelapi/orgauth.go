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
	Org    *paneldb.Org
	Role   paneldb.Role
	Grants []paneldb.Grant
}

func withOrgAccess(ctx context.Context, a *orgAccess) context.Context {
	return context.WithValue(ctx, orgContextKey, a)
}

func orgAccessFromContext(ctx context.Context) *orgAccess {
	a, _ := ctx.Value(orgContextKey).(*orgAccess)
	return a
}

// twoFactorRequiredMsg is the refusal resolveOrgAccess returns when the org (or, for a platform
// admin, the platform) requires two-factor authentication and the user has not turned it on;
// writeOrgAccessError adds the machine-readable code to it.
const twoFactorRequiredMsg = "two-factor authentication is required here; turn it on under My account"

// writeOrgAccessError writes a resolveOrgAccess refusal.
func writeOrgAccessError(w http.ResponseWriter, status int, msg string) {
	if msg == twoFactorRequiredMsg {
		writeErrorCode(w, status, "two_factor_required", msg)
		return
	}
	writeError(w, status, msg)
}

func (s *Server) resolveOrgAccess(ctx context.Context, user *paneldb.User, slug string, min paneldb.Role) (*orgAccess, int, string) {
	return s.resolveOrgAccessWith(ctx, user, slug, min, true)
}

// resolveOrgAccessWith resolves the caller's access; enforce2FA=false skips the two-factor
// requirement (only for the page that tells a blocked user what to do).
func (s *Server) resolveOrgAccessWith(ctx context.Context, user *paneldb.User, slug string, min paneldb.Role, enforce2FA bool) (*orgAccess, int, string) {
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
	if enforce2FA {
		// After the membership check: a non-member still gets 404, never learns the org exists.
		blocked, err := s.twoFactorBlocked(ctx, user, org)
		if err != nil {
			return nil, http.StatusInternalServerError, err.Error()
		}
		if blocked {
			return nil, http.StatusForbidden, twoFactorRequiredMsg
		}
	}
	if !role.AtLeast(min) {
		return nil, http.StatusForbidden, "insufficient role in this organization"
	}
	var grants []paneldb.Grant
	if role == paneldb.RoleMember {
		if grants, err = s.DB.ListMemberGrants(ctx, org.ID, user.ID); err != nil {
			return nil, http.StatusInternalServerError, err.Error()
		}
	}
	return &orgAccess{Org: org, Role: role, Grants: grants}, 0, ""
}

func (s *Server) requireOrgRole(min paneldb.Role, next http.Handler) http.Handler {
	return s.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		acc, status, msg := s.resolveOrgAccess(r.Context(), userFromContext(r.Context()), r.PathValue("org"), min)
		if acc == nil {
			writeOrgAccessError(w, status, msg)
			return
		}
		r.SetPathValue("namespace", gameserversv1alpha1.TenantNamespace(acc.Org.Slug))
		next.ServeHTTP(w, r.WithContext(withOrgAccess(r.Context(), acc)))
	}))
}
