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
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	if req.Name == "" && req.Spec.DisplayName == "" {
		writeError(w, http.StatusBadRequest, "name or displayName is required")
		return
	}
	if err := validateDisplayName(req.Spec.DisplayName); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	acc := orgAccessFromContext(r.Context())
	ns := r.PathValue("namespace")

	if req.Name == "" {
		// The identifier is derived from the display name; the user never types it.
		var existing gameserversv1alpha1.GameServerList
		if err := s.Client.List(r.Context(), &existing, client.InNamespace(ns)); err != nil {
			writeError(w, statusFor(err), err.Error())
			return
		}
		taken := make(map[string]bool, len(existing.Items))
		for i := range existing.Items {
			taken[existing.Items[i].Name] = true
		}
		req.Name = uniqueSlug(slugify(req.Spec.DisplayName), taken)
	}
	if err := s.checkAgainstEgg(r.Context(), ns, req.Spec.EggRef, req.Spec.ImageName, req.Spec.StartCommand, req.Spec.Variables); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	normalizeResources(&req.Spec.Resources)

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

// updateGameServerRequest carries only what the Panel lets an org admin change after creation. A
// nil field is left as it is; Variables replaces the whole override list. Disk (spec.storage) is
// deliberately absent: it is fixed at creation.
type updateGameServerRequest struct {
	DisplayName           *string                                   `json:"displayName"`
	ImageName             *string                                   `json:"imageName"`
	StartCommand          *string                                   `json:"startCommand"`
	BackupTarget          *gameserversv1alpha1.BackupTarget         `json:"backupTarget"`
	Variables             *[]gameserversv1alpha1.GameServerVariable `json:"variables"`
	Resources             *corev1.ResourceRequirements              `json:"resources"`
	PublicExposureEnabled *bool                                     `json:"publicExposureEnabled"`
}

func (s *Server) handleUpdateGameServer(w http.ResponseWriter, r *http.Request) {
	var req updateGameServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}

	const attempts = 4
	for attempt := 1; ; attempt++ {
		gs, code, err := s.applyGameServerUpdate(r, req)
		if err == nil {
			writeJSON(w, http.StatusOK, gs)
			return
		}
		if apierrors.IsConflict(err) && attempt < attempts {
			continue
		}
		writeError(w, code, err.Error())
		return
	}
}

// applyGameServerUpdate reads the server, applies the requested changes and writes it back. On error
// the int is the HTTP status to answer with.
func (s *Server) applyGameServerUpdate(r *http.Request, req updateGameServerRequest) (*gameserversv1alpha1.GameServer, int, error) {
	acc := orgAccessFromContext(r.Context())
	ns := r.PathValue("namespace")

	var gs gameserversv1alpha1.GameServer
	if err := s.Client.Get(r.Context(), gameServerKey(r), &gs); err != nil {
		return nil, statusFor(err), err
	}
	if req.DisplayName != nil {
		if err := validateDisplayName(*req.DisplayName); err != nil {
			return nil, http.StatusBadRequest, err
		}
		gs.Spec.DisplayName = *req.DisplayName
	}
	if req.ImageName != nil || req.Variables != nil || req.StartCommand != nil {
		imageName, startCommand, vars := gs.Spec.ImageName, gs.Spec.StartCommand, gs.Spec.Variables
		if req.ImageName != nil {
			imageName = *req.ImageName
		}
		if req.StartCommand != nil {
			startCommand = *req.StartCommand
		}
		if req.Variables != nil {
			vars = *req.Variables
		}
		if err := s.checkAgainstEgg(r.Context(), ns, gs.Spec.EggRef, imageName, startCommand, vars); err != nil {
			return nil, http.StatusUnprocessableEntity, err
		}
		gs.Spec.ImageName, gs.Spec.StartCommand, gs.Spec.Variables = imageName, startCommand, vars
	}
	if req.BackupTarget != nil {
		target, code, err := s.validateBackupTarget(r.Context(), ns, acc.Org.Slug, req.BackupTarget)
		if err != nil {
			return nil, code, err
		}
		gs.Spec.BackupTarget = target
	}
	if req.Resources != nil {
		gs.Spec.Resources = *req.Resources
		normalizeResources(&gs.Spec.Resources)
		if err := s.checkQuotaForUpdate(r.Context(), acc.Org.Slug, ns, gs.Name, gs.Spec); err != nil {
			return nil, http.StatusConflict, err
		}
	}
	if req.PublicExposureEnabled != nil {
		gs.Spec.PublicExposure.Enabled = *req.PublicExposureEnabled
	}
	if err := s.Client.Update(r.Context(), &gs); err != nil {
		return nil, statusFor(err), err
	}
	return &gs, http.StatusOK, nil
}

const maxDisplayNameRunes = 64

func validateDisplayName(name string) error {
	if utf8.RuneCountInString(name) > maxDisplayNameRunes {
		return fmt.Errorf("displayName must have at most %d characters", maxDisplayNameRunes)
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return fmt.Errorf("displayName must not contain control characters")
		}
	}
	return nil
}

// normalizeResources makes requests equal to limits (guaranteed QoS, and requests are what the
// org's ResourceQuota counts) whenever limits are given.
func normalizeResources(r *corev1.ResourceRequirements) {
	if len(r.Limits) > 0 {
		r.Requests = r.Limits.DeepCopy()
	}
}

// checkAgainstEgg validates the chosen image and the variable overrides against the referenced Egg
// so a bad value gets a clear 422. An Egg that cannot be found is left to the admission webhook, which is the authority.
func (s *Server) checkAgainstEgg(ctx context.Context, ns string, ref gameserversv1alpha1.GameServerEggRef, imageName, startCommand string, vars []gameserversv1alpha1.GameServerVariable) error {
	eggNS := ns
	if ref.Scope == gameserversv1alpha1.EggScopeCatalog {
		eggNS = gameserversv1alpha1.CatalogNamespace
	}
	var egg gameserversv1alpha1.Egg
	if err := s.Client.Get(ctx, client.ObjectKey{Namespace: eggNS, Name: ref.Name}, &egg); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	var msgs []string
	if _, ok := egg.ResolveImage(imageName); !ok {
		msgs = append(msgs, fmt.Sprintf("image %q is not declared by egg %q", imageName, egg.Name))
	}
	msgs = append(msgs, gameserversv1alpha1.ValidateStartCommand(&egg, startCommand)...)
	msgs = append(msgs, gameserversv1alpha1.ValidateVariableOverrides(&egg, vars)...)
	if len(msgs) > 0 {
		return fmt.Errorf("%s", strings.Join(msgs, "; "))
	}
	return nil
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

// unlessSuspended locks a suspended server for the organization: 423 with the admin's reason. Platform
// admins are not locked out, since they need to investigate and clean up.
func (s *Server) unlessSuspended(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u := userFromContext(r.Context()); u == nil || !u.IsAdmin {
			var gs gameserversv1alpha1.GameServer
			if err := s.Client.Get(r.Context(), gameServerKey(r), &gs); err == nil && gs.Spec.Suspended {
				msg := "this server is suspended"
				if gs.Spec.SuspendReason != "" {
					msg += ": " + gs.Spec.SuspendReason
				}
				writeError(w, http.StatusLocked, msg)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

type suspendRequest struct {
	Reason string `json:"reason"`
}

// handleSuspendGameServer (platform admin only) blocks a server: it is stopped and the organization
// can do nothing with it until it is unsuspended.
func (s *Server) handleSuspendGameServer(w http.ResponseWriter, r *http.Request) {
	var req suspendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		writeError(w, http.StatusBadRequest, "a reason is required")
		return
	}
	if utf8.RuneCountInString(reason) > 256 {
		writeError(w, http.StatusBadRequest, "reason must have at most 256 characters")
		return
	}
	for _, c := range reason {
		if unicode.IsControl(c) {
			writeError(w, http.StatusBadRequest, "reason must not contain control characters")
			return
		}
	}
	s.mutateGameServer(w, r, http.StatusAccepted, func(gs *gameserversv1alpha1.GameServer) {
		gs.Spec.Suspended = true
		gs.Spec.SuspendReason = reason
		gs.Spec.State = gameserversv1alpha1.GameServerStateStopped
	})
}

// handleUnsuspendGameServer lifts the block. It does not start the server: it stays stopped until
// someone in the organization starts it.
func (s *Server) handleUnsuspendGameServer(w http.ResponseWriter, r *http.Request) {
	s.mutateGameServer(w, r, http.StatusAccepted, func(gs *gameserversv1alpha1.GameServer) {
		gs.Spec.Suspended = false
		gs.Spec.SuspendReason = ""
	})
}

// mutateGameServer reads the server, applies mutate and writes it back, retrying when the controller
// wrote in between (the edit is field-level, so re-applying it is safe).
func (s *Server) mutateGameServer(w http.ResponseWriter, r *http.Request, okStatus int, mutate func(*gameserversv1alpha1.GameServer)) {
	const attempts = 4
	for attempt := 1; ; attempt++ {
		var gs gameserversv1alpha1.GameServer
		if err := s.Client.Get(r.Context(), gameServerKey(r), &gs); err != nil {
			writeError(w, statusFor(err), err.Error())
			return
		}
		mutate(&gs)
		err := s.Client.Update(r.Context(), &gs)
		if err == nil {
			writeJSON(w, okStatus, gs)
			return
		}
		if apierrors.IsConflict(err) && attempt < attempts {
			continue
		}
		writeError(w, statusFor(err), err.Error())
		return
	}
}

// handleReinstallGameServer makes the Egg's install script run again: it bumps spec.installRevision,
// which the install container compares with the marker on the data volume. A running server is
// restarted so the new revision takes effect now; a stopped one picks it up on its next start.
// Files are not deleted; only the script runs again.
func (s *Server) handleReinstallGameServer(w http.ResponseWriter, r *http.Request) {
	const attempts = 4
	for attempt := 1; ; attempt++ {
		var gs gameserversv1alpha1.GameServer
		if err := s.Client.Get(r.Context(), gameServerKey(r), &gs); err != nil {
			writeError(w, statusFor(err), err.Error())
			return
		}
		gs.Spec.InstallRevision++
		if gs.Spec.State == gameserversv1alpha1.GameServerStateRunning {
			if gs.Annotations == nil {
				gs.Annotations = map[string]string{}
			}
			gs.Annotations[gameserversv1alpha1.RestartAnnotation] = time.Now().UTC().Format(time.RFC3339Nano)
		}
		err := s.Client.Update(r.Context(), &gs)
		if err == nil {
			writeJSON(w, http.StatusAccepted, gs)
			return
		}
		if apierrors.IsConflict(err) && attempt < attempts {
			continue
		}
		writeError(w, statusFor(err), err.Error())
		return
	}
}

func gameServerKey(r *http.Request) client.ObjectKey {
	return client.ObjectKey{Namespace: r.PathValue("namespace"), Name: r.PathValue("name")}
}
