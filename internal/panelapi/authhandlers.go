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
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

// sessionTTL is how long a login session lasts before it needs renewing —
// there's no refresh-token flow yet, just log in again.
const sessionTTL = 24 * time.Hour

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type userResponse struct {
	ID                int64  `json:"id"`
	Username          string `json:"username"`
	Email             string `json:"email"`
	IsAdmin           bool   `json:"isAdmin"`
	DisplayName       string `json:"displayName"`
	Locale            string `json:"locale"`
	TimeZone          string `json:"timeZone"`
	Discord           string `json:"discord"`
	MinecraftUsername string `json:"minecraftUsername"`
	SteamID           string `json:"steamId"`
}

type loginResponse struct {
	Token     string       `json:"token"`
	ExpiresAt time.Time    `json:"expiresAt"`
	User      userResponse `json:"user"`
}

func toUserResponse(u *paneldb.User) userResponse {
	return userResponse{ID: u.ID, Username: u.Username, Email: u.Email, IsAdmin: u.IsAdmin,
		DisplayName: u.DisplayName, Locale: u.Locale, TimeZone: u.TimeZone, Discord: u.Discord,
		MinecraftUsername: u.MinecraftUsername, SteamID: u.SteamID}
}

func (s *Server) limiterKey(ctx context.Context, login string) string {
	if strings.Contains(login, "@") {
		if u, err := s.DB.GetUserByEmail(ctx, login); err == nil {
			return u.Username
		}
	}
	return strings.ToLower(login)
}

var authLog = logf.Log.WithName("panelapi-auth")

// maxLoggedUsername caps how much of an attacker-controlled username is
// stored in audit events and logs.
const maxLoggedUsername = 128

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	ip := clientIP(r, s.TrustedProxies)
	loggedName := truncate(req.Username, maxLoggedUsername)
	key := s.limiterKey(r.Context(), req.Username)

	// The limiter is asked BEFORE the password is checked: a blocked client
	// must not be able to learn whether a guess was right. A limiter error
	// fails open — a Redis outage should not lock everyone out of the panel —
	// and is logged so the gap in protection is visible.
	blocked, retryAfter, err := s.LoginLimiter.Blocked(r.Context(), key, ip)
	if err != nil {
		authLog.Error(err, "login rate limiter unavailable, failing open")
	} else if blocked {
		s.auditEvent(r, "", "login.blocked", "user", loggedName, "denied", nil)
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retryAfter.Seconds()))))
		writeError(w, http.StatusTooManyRequests, "too many failed login attempts, try again later")
		return
	}

	user, err := s.DB.VerifyPassword(r.Context(), req.Username, req.Password)
	if err != nil {
		if rerr := s.LoginLimiter.RecordFailure(r.Context(), key, ip); rerr != nil {
			authLog.Error(rerr, "could not record failed login")
		}
		s.auditEvent(r, "", "login.failure", "user", loggedName, "denied", nil)
		writeError(w, http.StatusUnauthorized, "invalid username, e-mail or password")
		return
	}
	if rerr := s.LoginLimiter.RecordSuccess(r.Context(), key, ip); rerr != nil {
		authLog.Error(rerr, "could not reset failed-login counter")
	}

	token, expiresAt, err := s.DB.CreateSession(r.Context(), user.ID, sessionTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditEventAs(r, user, "", "login.success", "user", user.Username, "success", nil)
	writeJSON(w, http.StatusOK, loginResponse{Token: token, ExpiresAt: expiresAt, User: toUserResponse(user)})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	// requireAuth already validated this token to get here; re-extract it
	// rather than threading it through the request context alongside the
	// user, since only this one handler needs the raw token.
	parts := strings.SplitN(r.Header.Get("Authorization"), " ", 2)
	if err := s.DB.RevokeSession(r.Context(), parts[1]); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, toUserResponse(userFromContext(r.Context())))
}
