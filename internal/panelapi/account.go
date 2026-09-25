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
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/kevinfinalboss/Hatchery/internal/mailer"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

func (s *Server) handleFeatures(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"passwordReset": s.mailEnabled()})
}

// profilePatch: nil fields are left unchanged, "" clears.
type profilePatch struct {
	DisplayName       *string `json:"displayName"`
	Locale            *string `json:"locale"`
	TimeZone          *string `json:"timeZone"`
	Discord           *string `json:"discord"`
	MinecraftUsername *string `json:"minecraftUsername"`
	SteamID           *string `json:"steamId"`
}

func (p profilePatch) apply(cur paneldb.Profile) paneldb.Profile {
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	set(&cur.DisplayName, p.DisplayName)
	set(&cur.Locale, p.Locale)
	set(&cur.TimeZone, p.TimeZone)
	set(&cur.Discord, p.Discord)
	set(&cur.MinecraftUsername, p.MinecraftUsername)
	set(&cur.SteamID, p.SteamID)
	return cur
}

func (s *Server) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	var req profilePatch
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	p := req.apply(user.Profile)
	if err := validateProfile(p); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := s.DB.UpdateProfile(r.Context(), user.ID, p); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditEvent(r, "", "user.profile.update", "user", user.Username, "success", nil)
	user.Profile = p
	writeJSON(w, http.StatusOK, toUserResponse(user))
}

// checkCurrentPassword verifies the signed-in user's password for sensitive
// changes. A wrong password counts against the login limiter, so this route
// cannot be used to brute-force a stolen session's password. It writes the
// error response and returns false on any failure.
func (s *Server) checkCurrentPassword(w http.ResponseWriter, r *http.Request, user *paneldb.User, password string) bool {
	ip := clientIP(r, s.TrustedProxies)
	blocked, retryAfter, err := s.LoginLimiter.Blocked(r.Context(), user.Username, ip)
	if err != nil {
		authLog.Error(err, "login rate limiter unavailable, failing open")
	} else if blocked {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retryAfter.Seconds()))))
		writeError(w, http.StatusTooManyRequests, "too many failed attempts, try again later")
		return false
	}
	if err := s.DB.VerifyUserPassword(r.Context(), user.ID, password); err != nil {
		if errors.Is(err, paneldb.ErrNotFound) {
			if rerr := s.LoginLimiter.RecordFailure(r.Context(), user.Username, ip); rerr != nil {
				authLog.Error(rerr, "could not record failed password check")
			}
			writeError(w, http.StatusForbidden, "current password is wrong")
			return false
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return false
	}
	return true
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	var req changePasswordRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if err := validatePassword(req.NewPassword); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if !s.checkCurrentPassword(w, r, user, req.CurrentPassword) {
		return
	}
	if err := s.DB.SetPassword(r.Context(), user.ID, req.NewPassword); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.DB.RevokeUserSessions(r.Context(), user.ID, headerToken(r)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditEvent(r, "", "user.password.change", "user", user.Username, "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

const (
	resetTokenTTL       = time.Hour
	emailChangeTokenTTL = 24 * time.Hour
	// forgot-password limits: every request may send an e-mail.
	forgotPerEmail = 5
	forgotPerIP    = 20
	forgotWindow   = time.Hour
)

type forgotPasswordRequest struct {
	Email string `json:"email"`
}

// handleForgotPassword always answers 202, account or not, and mails in the
// background, so neither the answer nor its timing tells whether an e-mail has
// an account. The per-e-mail limit applies to unknown e-mails too, for the
// same reason.
func (s *Server) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	if !s.mailEnabled() {
		writeError(w, http.StatusServiceUnavailable, "e-mail is not configured on this panel")
		return
	}
	var req forgotPasswordRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	email, err := validateEmail(req.Email)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	ip := clientIP(r, s.TrustedProxies)
	for _, l := range []struct {
		key   string
		limit int64
	}{{"pwreset:ip:" + ip, forgotPerIP}, {"pwreset:email:" + email, forgotPerEmail}} {
		ok, retryAfter, err := s.RequestLimiter.Allow(r.Context(), l.key, l.limit, forgotWindow)
		if err != nil {
			authLog.Error(err, "forgot-password limiter unavailable, failing open")
			continue
		}
		if !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retryAfter.Seconds()))))
			writeError(w, http.StatusTooManyRequests, "too many requests, try again later")
			return
		}
	}
	s.auditEventAs(r, nil, "", "user.password.forgot", "email", email, "success", nil)

	ctx := context.WithoutCancel(r.Context())
	s.runBackground(func() {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		user, err := s.DB.GetUserByEmail(ctx, email)
		if err != nil {
			if !errors.Is(err, paneldb.ErrNotFound) {
				mailLog.Error(err, "forgot password: looking up account")
			}
			return
		}
		token, err := s.DB.IssueUserToken(ctx, user.ID, paneldb.PurposePasswordReset, "", resetTokenTTL)
		if err != nil {
			mailLog.Error(err, "forgot password: issuing token")
			return
		}
		if err := s.sendMail(ctx, user.Email, user.Locale, mailer.KindPasswordReset,
			mailer.Data{Link: s.link("reset-password", token), Username: user.Username}); err != nil {
			mailLog.Error(err, "forgot password: sending e-mail")
		}
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "if the e-mail has an account, a link was sent"})
}

type resetPasswordRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	var req resetPasswordRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if err := validatePassword(req.Password); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	user, err := s.DB.ResetPassword(r.Context(), req.Token, req.Password)
	if err != nil {
		if errors.Is(err, paneldb.ErrInvalidToken) {
			writeError(w, http.StatusBadRequest, "invalid or expired link")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditEventAs(r, user, "", "user.password.reset", "user", user.Username, "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

type changeEmailRequest struct {
	NewEmail        string `json:"newEmail"`
	CurrentPassword string `json:"currentPassword"`
}

// handleChangeEmail only mails a confirmation link to the new address: the
// change applies when that link is opened. Otherwise whoever holds an open
// session could move the account to their own address and take it over via
// "forgot password".
//
// Without e-mail configured there is no confirmation link to send — and no
// "forgot password" either, which is what the confirmation protects — so the
// change applies at once (after the password check) and answers 200.
func (s *Server) handleChangeEmail(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	var req changeEmailRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	email, err := validateEmail(req.NewEmail)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if email == user.Email {
		writeError(w, http.StatusUnprocessableEntity, "that is already your e-mail")
		return
	}
	if !s.checkCurrentPassword(w, r, user, req.CurrentPassword) {
		return
	}
	if _, err := s.DB.GetUserByEmail(r.Context(), email); err == nil {
		writeError(w, http.StatusConflict, "e-mail already in use")
		return
	} else if !errors.Is(err, paneldb.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !s.mailEnabled() {
		updated, err := s.DB.SetEmail(r.Context(), user.ID, email)
		if err != nil {
			if errors.Is(err, paneldb.ErrEmailTaken) {
				writeError(w, http.StatusConflict, "e-mail already in use")
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.auditEvent(r, "", "user.email.change.confirm", "user", user.Username, "success", map[string]string{"newEmail": email, "confirmation": "none"})
		writeJSON(w, http.StatusOK, toUserResponse(updated))
		return
	}
	token, err := s.DB.IssueUserToken(r.Context(), user.ID, paneldb.PurposeEmailChange, email, emailChangeTokenTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.sendMail(r.Context(), email, user.Locale, mailer.KindEmailChange,
		mailer.Data{Link: s.link("confirm-email", token), Username: user.Username}); err != nil {
		mailLog.Error(err, "e-mail change: sending confirmation")
		writeError(w, http.StatusBadGateway, "could not send the confirmation e-mail")
		return
	}
	if err := s.sendMail(r.Context(), user.Email, user.Locale, mailer.KindEmailChangeNotice,
		mailer.Data{Username: user.Username, NewEmail: email}); err != nil {
		mailLog.Error(err, "e-mail change: sending notice to the old address")
	}
	s.auditEvent(r, "", "user.email.change.request", "user", user.Username, "success", map[string]string{"newEmail": email})
	w.WriteHeader(http.StatusAccepted)
}

type tokenRequest struct {
	Token string `json:"token"`
}

func (s *Server) handleConfirmEmail(w http.ResponseWriter, r *http.Request) {
	var req tokenRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	user, err := s.DB.ConfirmEmailChange(r.Context(), req.Token)
	if err != nil {
		switch {
		case errors.Is(err, paneldb.ErrInvalidToken):
			writeError(w, http.StatusBadRequest, "invalid or expired link")
		case errors.Is(err, paneldb.ErrEmailTaken):
			writeError(w, http.StatusConflict, "e-mail already in use")
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	s.auditEventAs(r, user, "", "user.email.change.confirm", "user", user.Username, "success", nil)
	writeJSON(w, http.StatusOK, toUserResponse(user))
}
