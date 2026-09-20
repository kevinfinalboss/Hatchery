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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

// handleListGameServers lists the GameServers of the requested org. The
// namespace is the org's derived one (set by requireOrgRole), so there is no
// way to list another org's servers from here.
func (s *Server) handleListGameServers(w http.ResponseWriter, r *http.Request) {
	var list gameserversv1alpha1.GameServerList
	if err := s.Client.List(r.Context(), &list, client.InNamespace(r.PathValue("namespace"))); err != nil {
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

// createGameServerRequest is a thin envelope around GameServerSpec. There is
// deliberately no namespace field: the server is always created in the org's
// own namespace. Any "namespace" a client sends is ignored by the JSON decoder.
type createGameServerRequest struct {
	Name string                             `json:"name"`
	Spec gameserversv1alpha1.GameServerSpec `json:"spec"`
}

func (s *Server) handleCreateGameServer(w http.ResponseWriter, r *http.Request) {
	var req createGameServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	acc := orgAccessFromContext(r.Context())
	ns := r.PathValue("namespace")

	if err := s.checkQuotaHeadroom(r.Context(), acc.Org.Slug, ns, req.Spec); err != nil {
		s.auditEvent(r, acc.Org.Slug, "gameserver.create", "gameserver", req.Name, "failed", nil)
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	gs := &gameserversv1alpha1.GameServer{
		ObjectMeta: metav1.ObjectMeta{Name: req.Name, Namespace: ns},
		Spec:       req.Spec,
	}
	// Validation beyond well-formed JSON (does the Egg exist, is eggRef/storage
	// well-formed) stays in the GameServer webhook and the CRD's CEL rules.
	if err := s.Client.Create(r.Context(), gs); err != nil {
		s.auditEvent(r, acc.Org.Slug, "gameserver.create", "gameserver", req.Name, "failed", nil)
		writeError(w, statusFor(err), err.Error())
		return
	}
	s.auditEvent(r, acc.Org.Slug, "gameserver.create", "gameserver", req.Name, "success", nil)
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

func (s *Server) handleRestartGameServer(w http.ResponseWriter, r *http.Request) {
	if !s.requireGameServerAccess(w, r) {
		return
	}

	var gs gameserversv1alpha1.GameServer
	if err := s.Client.Get(r.Context(), gameServerKey(r), &gs); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	if gs.Spec.State != gameserversv1alpha1.GameServerStateRunning {
		writeError(w, http.StatusConflict, "server is not running; start it instead")
		return
	}

	if gs.Annotations == nil {
		gs.Annotations = map[string]string{}
	}
	gs.Annotations[gameserversv1alpha1.RestartAnnotation] = time.Now().UTC().Format(time.RFC3339Nano)
	if err := s.Client.Update(r.Context(), &gs); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, gs)
}

func gameServerKey(r *http.Request) client.ObjectKey {
	return client.ObjectKey{Namespace: r.PathValue("namespace"), Name: r.PathValue("name")}
}
