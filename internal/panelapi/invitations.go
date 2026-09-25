package panelapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/kevinfinalboss/Hatchery/internal/mailer"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

const invitationTTL = 7 * 24 * time.Hour

type inviteRequest struct {
	Email string       `json:"email"`
	Role  paneldb.Role `json:"role"`
}

type inviteResponse struct {
	Invitation *paneldb.Invitation `json:"invitation"`
	InviteURL  string              `json:"inviteUrl,omitempty"`
}

// deliverInvitation e-mails inv, or — when e-mail is off — returns the link
// for the inviter to pass on. A send failure is returned to the caller.
func (s *Server) deliverInvitation(ctx context.Context, inv *paneldb.Invitation, token string) (inviteURL string, err error) {
	link := s.link("invite", token)
	if !s.mailEnabled() {
		return link, nil
	}
	return "", s.sendMail(ctx, inv.Email, inv.Locale, mailer.KindInvitation, mailer.Data{
		Link: link, OrgName: inv.OrgName, Role: string(inv.Role), InvitedBy: inv.InvitedBy})
}

func invitationID(r *http.Request) (int64, error) { return strconv.ParseInt(r.PathValue("id"), 10, 64) }

func (s *Server) handleListInvitations(w http.ResponseWriter, r *http.Request) {
	acc := orgAccessFromContext(r.Context())
	list, err := s.DB.ListInvitations(r.Context(), acc.Org.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []paneldb.Invitation{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateInvitation(w http.ResponseWriter, r *http.Request) {
	acc := orgAccessFromContext(r.Context())
	inviter := userFromContext(r.Context())
	var req inviteRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if !req.Role.Valid() {
		writeError(w, http.StatusBadRequest, `role must be "owner", "admin" or "member"`)
		return
	}
	if !canTouchRole(acc.Role, req.Role) {
		writeError(w, http.StatusForbidden, "only an owner can invite another owner")
		return
	}
	email, err := validateEmail(req.Email)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if existing, err := s.DB.GetUserByEmail(r.Context(), email); err == nil {
		if _, err := s.DB.GetMembership(r.Context(), acc.Org.ID, existing.ID); err == nil {
			writeError(w, http.StatusConflict, "this person is already a member")
			return
		}
	} else if !errors.Is(err, paneldb.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	inv, token, err := s.DB.CreateInvitation(r.Context(), acc.Org.ID, email, req.Role, inviter.Locale, inviter.ID, invitationTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	inviteURL, err := s.deliverInvitation(r.Context(), inv, token)
	if err != nil {
		mailLog.Error(err, "sending invitation", "org", acc.Org.Slug)
		// Nothing stays pending without an e-mail behind it.
		_ = s.DB.DeleteInvitation(r.Context(), acc.Org.ID, inv.ID)
		s.auditEvent(r, acc.Org.Slug, "invitation.create", "email", email, "failed", map[string]string{"role": string(req.Role)})
		writeError(w, http.StatusBadGateway, "could not send the invitation e-mail")
		return
	}
	s.auditEvent(r, acc.Org.Slug, "invitation.create", "email", email, "success", map[string]string{"role": string(req.Role)})
	writeJSON(w, http.StatusCreated, inviteResponse{Invitation: inv, InviteURL: inviteURL})
}

func (s *Server) handleResendInvitation(w http.ResponseWriter, r *http.Request) {
	acc := orgAccessFromContext(r.Context())
	id, err := invitationID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid invitation id")
		return
	}
	inv, token, err := s.DB.ReissueInvitation(r.Context(), acc.Org.ID, id, invitationTTL)
	if err != nil {
		if errors.Is(err, paneldb.ErrNotFound) {
			writeError(w, http.StatusNotFound, "invitation not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !canTouchRole(acc.Role, inv.Role) {
		writeError(w, http.StatusForbidden, "only an owner can resend an owner invitation")
		return
	}
	inviteURL, err := s.deliverInvitation(r.Context(), inv, token)
	if err != nil {
		mailLog.Error(err, "resending invitation", "org", acc.Org.Slug)
		writeError(w, http.StatusBadGateway, "could not send the invitation e-mail")
		return
	}
	s.auditEvent(r, acc.Org.Slug, "invitation.resend", "email", inv.Email, "success", nil)
	writeJSON(w, http.StatusOK, inviteResponse{Invitation: inv, InviteURL: inviteURL})
}

func (s *Server) handleDeleteInvitation(w http.ResponseWriter, r *http.Request) {
	acc := orgAccessFromContext(r.Context())
	id, err := invitationID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid invitation id")
		return
	}
	if err := s.DB.DeleteInvitation(r.Context(), acc.Org.ID, id); err != nil {
		if errors.Is(err, paneldb.ErrNotFound) {
			writeError(w, http.StatusNotFound, "invitation not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditEvent(r, acc.Org.Slug, "invitation.revoke", "invitation", strconv.FormatInt(id, 10), "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

type invitationPreview struct {
	OrgName       string       `json:"orgName"`
	OrgSlug       string       `json:"orgSlug"`
	Role          paneldb.Role `json:"role"`
	Email         string       `json:"email"`
	InvitedBy     string       `json:"invitedBy"`
	AccountExists bool         `json:"accountExists"`
}

func (s *Server) handleLookupInvitation(w http.ResponseWriter, r *http.Request) {
	var req tokenRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	inv, err := s.DB.GetInvitationByToken(r.Context(), req.Token)
	if err != nil {
		if errors.Is(err, paneldb.ErrInvalidToken) {
			writeError(w, http.StatusNotFound, "invalid or expired invitation")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, uerr := s.DB.GetUserByEmail(r.Context(), inv.Email)
	if uerr != nil && !errors.Is(uerr, paneldb.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, uerr.Error())
		return
	}
	writeJSON(w, http.StatusOK, invitationPreview{OrgName: inv.OrgName, OrgSlug: inv.OrgSlug, Role: inv.Role,
		Email: inv.Email, InvitedBy: inv.InvitedBy, AccountExists: uerr == nil})
}

type acceptInvitationRequest struct {
	Token       string `json:"token"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName"`
}

// handleAcceptInvitation: an e-mail with an account needs that account signed
// in (nobody joins an org without agreeing); an e-mail without one gets an
// account created here, already a member, and a session.
func (s *Server) handleAcceptInvitation(w http.ResponseWriter, r *http.Request) {
	var req acceptInvitationRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	inv, err := s.DB.GetInvitationByToken(r.Context(), req.Token)
	if err != nil {
		if errors.Is(err, paneldb.ErrInvalidToken) {
			writeError(w, http.StatusNotFound, "invalid or expired invitation")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	existing, err := s.DB.GetUserByEmail(r.Context(), inv.Email)
	switch {
	case err == nil:
		s.acceptAsExisting(w, r, req.Token, existing)
	case errors.Is(err, paneldb.ErrNotFound):
		s.acceptAsNew(w, r, req)
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func (s *Server) acceptAsExisting(w http.ResponseWriter, r *http.Request, token string, existing *paneldb.User) {
	caller := s.optionalUser(r)
	if caller == nil {
		writeError(w, http.StatusUnauthorized, "sign in with the invited account to accept")
		return
	}
	if caller.ID != existing.ID {
		writeError(w, http.StatusForbidden, "this invitation is for another account")
		return
	}
	inv, err := s.DB.AcceptInvitationExistingUser(r.Context(), token, caller.ID)
	if err != nil {
		switch {
		case errors.Is(err, paneldb.ErrInvalidToken):
			writeError(w, http.StatusNotFound, "invalid or expired invitation")
		case errors.Is(err, paneldb.ErrWrongAccount):
			writeError(w, http.StatusForbidden, "this invitation is for another account")
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	s.auditEventAs(r, caller, inv.OrgSlug, "invitation.accept", "user", caller.Username, "success", map[string]string{"role": string(inv.Role)})
	writeJSON(w, http.StatusOK, map[string]string{"orgSlug": inv.OrgSlug})
}

func (s *Server) acceptAsNew(w http.ResponseWriter, r *http.Request, req acceptInvitationRequest) {
	if err := validateUsername(req.Username); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := validatePassword(req.Password); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := validateDisplayName(req.DisplayName); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	user, inv, err := s.DB.AcceptInvitationNewUser(r.Context(), req.Token, req.Username, req.Password, req.DisplayName)
	if err != nil {
		switch {
		case errors.Is(err, paneldb.ErrInvalidToken):
			writeError(w, http.StatusNotFound, "invalid or expired invitation")
		case errors.Is(err, paneldb.ErrAlreadyExists):
			writeError(w, http.StatusConflict, "username already taken")
		case errors.Is(err, paneldb.ErrEmailTaken):
			writeError(w, http.StatusConflict, "an account with this e-mail already exists: sign in to accept")
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	token, expiresAt, err := s.DB.CreateSession(r.Context(), user.ID, sessionTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditEventAs(r, user, inv.OrgSlug, "invitation.accept", "user", user.Username, "success", map[string]string{"role": string(inv.Role), "newAccount": "true"})
	writeJSON(w, http.StatusCreated, loginResponse{Token: token, ExpiresAt: expiresAt, User: toUserResponse(user)})
}
