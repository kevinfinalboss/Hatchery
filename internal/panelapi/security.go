package panelapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

// settingRequireAdmin2FA is the platform_settings key of "platform admins must use 2FA".
const settingRequireAdmin2FA = "require_2fa_admins"

// adminNeeds2FA: u is a platform admin without two-factor authentication while the platform
// requires it.
func (s *Server) adminNeeds2FA(ctx context.Context, u *paneldb.User) (bool, error) {
	if !u.IsAdmin || u.TwoFactorEnabled {
		return false, nil
	}
	v, err := s.DB.GetSetting(ctx, settingRequireAdmin2FA)
	return v == "true", err
}

// twoFactorBlocked: the user may not use org until they turn two-factor on. A platform admin is
// owner of every org through the platform, so the platform's rule applies to them too.
func (s *Server) twoFactorBlocked(ctx context.Context, u *paneldb.User, org *paneldb.Org) (bool, error) {
	if u.TwoFactorEnabled {
		return false, nil
	}
	if org.Require2FA {
		return true, nil
	}
	return s.adminNeeds2FA(ctx, u)
}

type orgSecurityResponse struct {
	Require2FA        bool `json:"require2fa"`
	Blocked           bool `json:"blocked"`
	MembersWithout2FA *int `json:"membersWithout2fa,omitempty"`
}

// handleGetOrgSecurity is reachable while blocked (it is what the UI shows a blocked member).
func (s *Server) handleGetOrgSecurity(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	acc, status, msg := s.resolveOrgAccessWith(r.Context(), user, r.PathValue("org"), paneldb.RoleMember, false)
	if acc == nil {
		writeError(w, status, msg)
		return
	}
	blocked, err := s.twoFactorBlocked(r.Context(), user, acc.Org)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp := orgSecurityResponse{Require2FA: acc.Org.Require2FA, Blocked: blocked}
	if acc.Role.AtLeast(paneldb.RoleAdmin) && !blocked {
		n, err := s.DB.CountMembersWithout2FA(r.Context(), acc.Org.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		resp.MembersWithout2FA = &n
	}
	writeJSON(w, http.StatusOK, resp)
}

// handlePutOrgSecurity changes the org's requirement. It goes through the requirement itself, and
// the caller must have 2FA either way: someone holding an admin's password alone cannot turn the
// requirement off.
func (s *Server) handlePutOrgSecurity(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Require2FA *bool `json:"require2fa"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || req.Require2FA == nil {
		writeError(w, http.StatusBadRequest, "body must be {\"require2fa\": true|false}")
		return
	}
	user := userFromContext(r.Context())
	if !user.TwoFactorEnabled {
		writeError(w, http.StatusConflict, "turn on your own two-factor authentication before changing this")
		return
	}
	acc := orgAccessFromContext(r.Context())
	if err := s.DB.SetOrgRequire2FA(r.Context(), acc.Org.ID, *req.Require2FA); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditEvent(r, acc.Org.Slug, "org.security.update", "org", acc.Org.Slug, "success",
		map[string]string{"require2fa": strconv.FormatBool(*req.Require2FA)})
	acc.Org.Require2FA = *req.Require2FA
	writeJSON(w, http.StatusOK, orgSecurityResponse{Require2FA: acc.Org.Require2FA})
}

type platformSecurityResponse struct {
	RequireAdminTwoFactor bool `json:"requireAdminTwoFactor"`
	Blocked               bool `json:"blocked"`
}

// handleGetPlatformSecurity is reachable by a blocked platform admin.
func (s *Server) handleGetPlatformSecurity(w http.ResponseWriter, r *http.Request) {
	v, err := s.DB.GetSetting(r.Context(), settingRequireAdmin2FA)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	needs, _ := s.adminNeeds2FA(r.Context(), userFromContext(r.Context()))
	writeJSON(w, http.StatusOK, platformSecurityResponse{RequireAdminTwoFactor: v == "true", Blocked: needs})
}

func (s *Server) handlePutPlatformSecurity(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RequireAdminTwoFactor *bool `json:"requireAdminTwoFactor"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || req.RequireAdminTwoFactor == nil {
		writeError(w, http.StatusBadRequest, "body must be {\"requireAdminTwoFactor\": true|false}")
		return
	}
	if !userFromContext(r.Context()).TwoFactorEnabled {
		writeError(w, http.StatusConflict, "turn on your own two-factor authentication before changing this")
		return
	}
	if err := s.DB.SetSetting(r.Context(), settingRequireAdmin2FA, strconv.FormatBool(*req.RequireAdminTwoFactor)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditEvent(r, "", "platform.security.update", "platform", "security", "success",
		map[string]string{"requireAdminTwoFactor": strconv.FormatBool(*req.RequireAdminTwoFactor)})
	writeJSON(w, http.StatusOK, platformSecurityResponse{RequireAdminTwoFactor: *req.RequireAdminTwoFactor})
}
