package panelapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

type setRoleRequest struct {
	Role paneldb.Role `json:"role"`
}

func memberUserID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("userId"), 10, 64)
}

// canTouchRole reports whether the caller may create, grant, change or remove
// a membership involving role. Only an owner (or a platform admin, who is an
// effective owner) may deal in owners: otherwise an org admin could promote
// themselves and lock the real owners out.
func canTouchRole(caller paneldb.Role, role paneldb.Role) bool {
	return role != paneldb.RoleOwner || caller == paneldb.RoleOwner
}

func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request) {
	acc := orgAccessFromContext(r.Context())
	members, err := s.DB.ListMembers(r.Context(), acc.Org.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if members == nil {
		members = []paneldb.Member{}
	}
	// E-mails are for those who manage the org (they need them to handle invitations).
	if !acc.Role.AtLeast(paneldb.RoleAdmin) {
		for i := range members {
			members[i].Email = ""
		}
	}
	writeJSON(w, http.StatusOK, members)
}

func (s *Server) handleSetMemberRole(w http.ResponseWriter, r *http.Request) {
	acc := orgAccessFromContext(r.Context())
	uid, err := memberUserID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	var req setRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if !req.Role.Valid() {
		writeError(w, http.StatusBadRequest, `role must be "owner", "admin" or "member"`)
		return
	}
	current, err := s.DB.GetMembership(r.Context(), acc.Org.ID, uid)
	if err != nil {
		if errors.Is(err, paneldb.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user is not a member of this organization")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !canTouchRole(acc.Role, current) || !canTouchRole(acc.Role, req.Role) {
		writeError(w, http.StatusForbidden, "only an owner can grant or change the owner role")
		return
	}
	if err := s.DB.SetMemberRole(r.Context(), acc.Org.ID, uid, req.Role); err != nil {
		if errors.Is(err, paneldb.ErrLastOwner) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditEvent(r, acc.Org.Slug, "member.role", "user", strconv.FormatInt(uid, 10), "success", map[string]string{"role": string(req.Role)})

	hasAccess := true
	if req.Role == paneldb.RoleMember {
		grants, err := s.DB.ListMemberGrants(r.Context(), acc.Org.ID, uid)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		hasAccess = len(grants) > 0
	}
	writeJSON(w, http.StatusOK, map[string]any{"role": req.Role, "hasServerAccess": hasAccess})
}

type memberGrantsBody struct {
	Grants []paneldb.Grant `json:"grants"`
}

func (s *Server) handleGetMemberPermissions(w http.ResponseWriter, r *http.Request) {
	acc := orgAccessFromContext(r.Context())
	uid, err := memberUserID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	if _, err := s.DB.GetMembership(r.Context(), acc.Org.ID, uid); err != nil {
		writeError(w, http.StatusNotFound, "user is not a member of this organization")
		return
	}
	grants, err := s.DB.ListMemberGrants(r.Context(), acc.Org.ID, uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if grants == nil {
		grants = []paneldb.Grant{}
	}
	writeJSON(w, http.StatusOK, memberGrantsBody{Grants: grants})
}

func (s *Server) handlePutMemberPermissions(w http.ResponseWriter, r *http.Request) {
	acc := orgAccessFromContext(r.Context())
	uid, err := memberUserID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	var body memberGrantsBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if _, err := s.DB.GetMembership(r.Context(), acc.Org.ID, uid); err != nil {
		writeError(w, http.StatusNotFound, "user is not a member of this organization")
		return
	}
	for _, g := range body.Grants {
		for _, p := range g.Permissions {
			if !p.Valid() {
				writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("unknown permission %q", p))
				return
			}
		}
		if g.GameServer == paneldb.AllServers {
			continue
		}
		var gs gameserversv1alpha1.GameServer
		key := client.ObjectKey{Namespace: r.PathValue("namespace"), Name: g.GameServer}
		if err := s.Client.Get(r.Context(), key, &gs); err != nil {
			if apierrors.IsNotFound(err) {
				writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("no game server named %q in this organization", g.GameServer))
				return
			}
			writeError(w, statusFor(err), err.Error())
			return
		}
	}
	if err := s.DB.SetMemberGrants(r.Context(), acc.Org.ID, uid, body.Grants); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	grants, err := s.DB.ListMemberGrants(r.Context(), acc.Org.ID, uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if grants == nil {
		grants = []paneldb.Grant{}
	}
	raw, _ := json.Marshal(grants)
	s.auditEvent(r, acc.Org.Slug, "member.permissions.update", "user", strconv.FormatInt(uid, 10), "success", map[string]string{"grants": string(raw)})
	writeJSON(w, http.StatusOK, memberGrantsBody{Grants: grants})
}

// handleRemoveMember lets any member leave, and lets an admin remove others.
func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	acc := orgAccessFromContext(r.Context())
	uid, err := memberUserID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	self := userFromContext(r.Context()).ID == uid
	if !self && !acc.Role.AtLeast(paneldb.RoleAdmin) {
		writeError(w, http.StatusForbidden, "only an admin can remove other members")
		return
	}
	current, err := s.DB.GetMembership(r.Context(), acc.Org.ID, uid)
	if err != nil {
		if errors.Is(err, paneldb.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user is not a member of this organization")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !self && !canTouchRole(acc.Role, current) {
		writeError(w, http.StatusForbidden, "only an owner can remove an owner")
		return
	}
	if err := s.DB.RemoveMember(r.Context(), acc.Org.ID, uid); err != nil {
		if errors.Is(err, paneldb.ErrLastOwner) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditEvent(r, acc.Org.Slug, "member.remove", "user", strconv.FormatInt(uid, 10), "success", nil)
	w.WriteHeader(http.StatusNoContent)
}
