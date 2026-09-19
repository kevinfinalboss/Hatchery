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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

// handleListGameServers returns every GameServer for an admin, or just the
// ones the requesting user has an explicit grant for otherwise. There's no
// way to express "namespace/name in this specific set" as a single List
// call against the Kubernetes API, so a non-admin's grants are fetched
// individually — fine at the scale this table is expected to stay at (see
// paneldb.ListGameServerAccess).
func (s *Server) handleListGameServers(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())

	if !user.IsAdmin {
		refs, err := s.DB.ListGameServerAccess(r.Context(), user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		list := gameserversv1alpha1.GameServerList{}
		for _, ref := range refs {
			var gs gameserversv1alpha1.GameServer
			if err := s.Client.Get(r.Context(), client.ObjectKey{Namespace: ref.Namespace, Name: ref.Name}, &gs); err != nil {
				if statusFor(err) == http.StatusNotFound {
					continue // grant outlived the GameServer it pointed at
				}
				writeError(w, statusFor(err), err.Error())
				return
			}
			list.Items = append(list.Items, gs)
		}
		writeJSON(w, http.StatusOK, list)
		return
	}

	var list gameserversv1alpha1.GameServerList
	var opts []client.ListOption
	if ns := r.URL.Query().Get("namespace"); ns != "" {
		opts = append(opts, client.InNamespace(ns))
	}
	if err := s.Client.List(r.Context(), &list, opts...); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGetGameServer(w http.ResponseWriter, r *http.Request) {
	if !s.requireGameServerAccess(w, r) {
		return
	}
	var gs gameserversv1alpha1.GameServer
	if err := s.Client.Get(r.Context(), gameServerKey(r), &gs); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, gs)
}

// createGameServerRequest is a thin envelope around GameServerSpec: the CRD
// type itself has no top-level name/namespace fields worth exposing
// separately in a create request body.
type createGameServerRequest struct {
	Name      string                             `json:"name"`
	Namespace string                             `json:"namespace"`
	Spec      gameserversv1alpha1.GameServerSpec `json:"spec"`
}

func (s *Server) handleCreateGameServer(w http.ResponseWriter, r *http.Request) {
	var req createGameServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if req.Name == "" || req.Namespace == "" {
		writeError(w, http.StatusBadRequest, "name and namespace are required")
		return
	}

	gs := &gameserversv1alpha1.GameServer{
		ObjectMeta: metav1.ObjectMeta{Name: req.Name, Namespace: req.Namespace},
		Spec:       req.Spec,
	}
	// Validation beyond "is this well-formed JSON" (does the Egg exist, is
	// eggRef/storage well-formed) is intentionally not duplicated here — the
	// GameServer validating webhook and CRD CEL rules already enforce it
	// server-side, and re-checking here would just be two sources of truth
	// that can drift.
	if err := s.Client.Create(r.Context(), gs); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, gs)
}

func (s *Server) handleDeleteGameServer(w http.ResponseWriter, r *http.Request) {
	gs := &gameserversv1alpha1.GameServer{ObjectMeta: metav1.ObjectMeta{
		Name:      r.PathValue("name"),
		Namespace: r.PathValue("namespace"),
	}}
	if err := s.Client.Delete(r.Context(), gs); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

type setStateRequest struct {
	State gameserversv1alpha1.GameServerState `json:"state"`
}

// handleSetGameServerState is the start/stop endpoint: it reads the current
// GameServer, flips spec.state, and lets the GameServerController do the
// actual work of creating or deleting the Pod.
func (s *Server) handleSetGameServerState(w http.ResponseWriter, r *http.Request) {
	if !s.requireGameServerAccess(w, r) {
		return
	}
	var req setStateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if req.State != gameserversv1alpha1.GameServerStateRunning && req.State != gameserversv1alpha1.GameServerStateStopped {
		writeError(w, http.StatusBadRequest, `state must be "Running" or "Stopped"`)
		return
	}

	var gs gameserversv1alpha1.GameServer
	key := gameServerKey(r)
	if err := s.Client.Get(r.Context(), key, &gs); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	gs.Spec.State = req.State
	if err := s.Client.Update(r.Context(), &gs); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, gs)
}

func gameServerKey(r *http.Request) client.ObjectKey {
	return client.ObjectKey{Namespace: r.PathValue("namespace"), Name: r.PathValue("name")}
}
