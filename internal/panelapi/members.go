package panelapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

type addMemberRequest struct {
	Username string       `json:"username"`
	Role     paneldb.Role `json:"role"`
}

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
	writeJSON(w, http.StatusOK, members)
}

// handleAddMember adds an existing user by username. Adding by username lets
// an org admin learn whether a username exists; that is accepted here because
// the alternative — listing every user — would be far worse.
func (s *Server) handleAddMember(w http.ResponseWriter, r *http.Request) {
	acc := orgAccessFromContext(r.Context())
	var req addMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if !req.Role.Valid() {
		writeError(w, http.StatusBadRequest, `role must be "owner", "admin" or "member"`)
		return
	}
	if !canTouchRole(acc.Role, req.Role) {
		writeError(w, http.StatusForbidden, "only an owner can add another owner")
		return
	}
	user, err := s.DB.GetUserByUsername(r.Context(), req.Username)
	if err != nil {
		if errors.Is(err, paneldb.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.DB.AddMember(r.Context(), acc.Org.ID, user.ID, req.Role); err != nil {
		if errors.Is(err, paneldb.ErrAlreadyExists) {
			writeError(w, http.StatusConflict, "user is already a member")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditEvent(r, acc.Org.Slug, "member.add", "user", user.Username, "success", map[string]string{"role": string(req.Role)})
	writeJSON(w, http.StatusCreated, paneldb.Member{UserID: user.ID, Username: user.Username, Role: req.Role})
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
	writeJSON(w, http.StatusOK, setRoleRequest{Role: req.Role})
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
