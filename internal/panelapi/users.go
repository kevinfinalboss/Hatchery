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
	"errors"
	"net/http"
	"strconv"

	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

// Every handler in this file is platform-admin-only (see Routes): accounts
// are platform-level, while who belongs to which org is managed in members.go.

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.DB.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp := make([]userResponse, len(users))
	for i, u := range users {
		resp[i] = toUserResponse(&u)
	}
	writeJSON(w, http.StatusOK, resp)
}

type createUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	IsAdmin  bool   `json:"isAdmin"`
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required")
		return
	}

	user, err := s.DB.CreateUser(r.Context(), req.Username, req.Password, req.IsAdmin)
	if err != nil {
		if errors.Is(err, paneldb.ErrAlreadyExists) {
			writeError(w, http.StatusConflict, "username already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toUserResponse(user))
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := userIDFromPath(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	target, err := s.DB.GetUser(r.Context(), id)
	if err != nil {
		if errors.Is(err, paneldb.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if target.Username == BootstrapAdminUsername {
		writeError(w, http.StatusConflict, "the initial admin user cannot be deleted")
		return
	}
	if err := s.DB.DeleteUser(r.Context(), id); err != nil {
		if errors.Is(err, paneldb.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		if errors.Is(err, paneldb.ErrLastOwner) {
			writeError(w, http.StatusConflict, "user is the only owner of an organization: transfer ownership first")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func userIDFromPath(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}
