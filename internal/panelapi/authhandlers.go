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
	"encoding/json"
	"net/http"
	"strings"
	"time"

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
	ID       int64  `json:"id"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"isAdmin"`
}

type loginResponse struct {
	Token     string       `json:"token"`
	ExpiresAt time.Time    `json:"expiresAt"`
	User      userResponse `json:"user"`
}

func toUserResponse(u *paneldb.User) userResponse {
	return userResponse{ID: u.ID, Username: u.Username, IsAdmin: u.IsAdmin}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}

	user, err := s.DB.VerifyPassword(r.Context(), req.Username, req.Password)
	if err != nil {
		// paneldb.VerifyPassword already collapses "no such user" and "wrong
		// password" into the same ErrNotFound — mirror that here rather than
		// distinguishing "not found" from "forbidden", so a 4xx-status
		// difference can't leak which one it was either.
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	token, expiresAt, err := s.DB.CreateSession(r.Context(), user.ID, sessionTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

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
