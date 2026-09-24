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
	"sort"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/backupplan"
)

// maxSchedulesPerServer bounds how many GameServerSchedules one server may have, independent of
// any platform-storage limit.
const maxSchedulesPerServer = 10

// scheduleWrite is the body of POST/PUT .../schedules[/{schedule}]. OnlyWhenRunning nil defaults
// to true (skip the whole run while the server is not Running), matching the CRD's default.
type scheduleWrite struct {
	DisplayName     string                             `json:"displayName"`
	Cron            string                             `json:"cron"`
	TimeZone        string                             `json:"timeZone"`
	Suspend         bool                               `json:"suspend"`
	OnlyWhenRunning *bool                              `json:"onlyWhenRunning"`
	Tasks           []gameserversv1alpha1.ScheduleTask `json:"tasks"`
}

type scheduleItem struct {
	Name   string                                       `json:"name"`
	Spec   gameserversv1alpha1.GameServerScheduleSpec   `json:"spec"`
	Status gameserversv1alpha1.GameServerScheduleStatus `json:"status"`
}

func toScheduleItem(sc *gameserversv1alpha1.GameServerSchedule) scheduleItem {
	return scheduleItem{Name: sc.Name, Spec: sc.Spec, Status: sc.Status}
}

// specFromWrite builds a GameServerScheduleSpec for the server named by the URL from a decoded
// scheduleWrite.
func specFromWrite(server string, req scheduleWrite) gameserversv1alpha1.GameServerScheduleSpec {
	onlyWhenRunning := true
	if req.OnlyWhenRunning != nil {
		onlyWhenRunning = *req.OnlyWhenRunning
	}
	return gameserversv1alpha1.GameServerScheduleSpec{
		GameServerRef:   gameserversv1alpha1.GameServerRef{Name: server},
		DisplayName:     req.DisplayName,
		Cron:            req.Cron,
		TimeZone:        req.TimeZone,
		Suspend:         req.Suspend,
		OnlyWhenRunning: onlyWhenRunning,
		Tasks:           req.Tasks,
	}
}

// listServerSchedules lists the schedules that belong to one GameServer. There is no field
// index for spec.gameServerRef.name (the fake client used in tests does not support one either),
// so this filters the namespace's list in memory, same as backupOfServer/serversUsing do for
// backups.
func (s *Server) listServerSchedules(ctx context.Context, ns, server string) ([]gameserversv1alpha1.GameServerSchedule, error) {
	var list gameserversv1alpha1.GameServerScheduleList
	if err := s.Client.List(ctx, &list, client.InNamespace(ns)); err != nil {
		return nil, err
	}
	out := make([]gameserversv1alpha1.GameServerSchedule, 0, len(list.Items))
	for _, it := range list.Items {
		if it.Spec.GameServerRef.Name == server {
			out = append(out, it)
		}
	}
	return out, nil
}

// scheduleOfServer finds a schedule by name and makes sure it belongs to the server in the URL,
// so a schedule is only ever reachable through its own server (same rule as backupOfServer).
func (s *Server) scheduleOfServer(r *http.Request) (*gameserversv1alpha1.GameServerSchedule, int, error) {
	var sc gameserversv1alpha1.GameServerSchedule
	key := client.ObjectKey{Namespace: r.PathValue("namespace"), Name: r.PathValue("schedule")}
	if err := s.Client.Get(r.Context(), key, &sc); err != nil {
		return nil, statusFor(err), err
	}
	if sc.Spec.GameServerRef.Name != r.PathValue("name") {
		return nil, http.StatusNotFound, fmt.Errorf("schedule not found for this server")
	}
	return &sc, 0, nil
}

func (s *Server) handleListSchedules(w http.ResponseWriter, r *http.Request) {
	items, err := s.listServerSchedules(r.Context(), r.PathValue("namespace"), r.PathValue("name"))
	if err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	out := make([]scheduleItem, 0, len(items))
	for i := range items {
		out = append(out, toScheduleItem(&items[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetSchedule(w http.ResponseWriter, r *http.Request) {
	sc, code, err := s.scheduleOfServer(r)
	if err != nil {
		writeError(w, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toScheduleItem(sc))
}

// scheduleBackupHeadroom checks, for a schedule targeting the org's platform backup storage, that
// every Backup task's keepLast leaves room for the next backup before the platform's
// per-server limit is hit. keepLast 0 (no rotation) always fails this once a limit applies,
// because the limit would then be hit on the very next backup with nothing to rotate out.
// Returns "" when there is nothing to reject (no Backup task, target isn't the platform, or the
// platform storage has no limits configured for this org).
func (s *Server) scheduleBackupHeadroom(ctx context.Context, gs *gameserversv1alpha1.GameServer, orgSlug string, spec gameserversv1alpha1.GameServerScheduleSpec) (string, error) {
	if gs.Spec.BackupTarget == nil || gs.Spec.BackupTarget.Connection != backupplan.PlatformConnection {
		return "", nil
	}
	hasBackup := false
	for _, t := range spec.Tasks {
		if t.Action == gameserversv1alpha1.ScheduleActionBackup {
			hasBackup = true
			break
		}
	}
	if !hasBackup {
		return "", nil
	}
	limits, err := s.planner().OrgLimits(ctx, orgSlug)
	if err != nil {
		return "", err
	}
	if limits == nil {
		return "", nil
	}
	for _, t := range spec.Tasks {
		if t.Action != gameserversv1alpha1.ScheduleActionBackup {
			continue
		}
		if t.KeepLast == 0 || t.KeepLast >= limits.MaxPerServer {
			return fmt.Sprintf("keepLast must be lower than this organization's platform limit of %d backups per server (the next backup needs room before an old one is rotated out)", limits.MaxPerServer), nil
		}
	}
	return "", nil
}

// createScheduleObject creates sc, retrying once with a random suffix if two schedules of the
// same server collide within the same second (ObjectName's resolution) — same pattern as
// backupplan.Planner.Create.
func (s *Server) createScheduleObject(ctx context.Context, sc *gameserversv1alpha1.GameServerSchedule) error {
	err := s.Client.Create(ctx, sc)
	if apierrors.IsAlreadyExists(err) {
		suffix, rerr := backupplan.RandomToken(3)
		if rerr != nil {
			return rerr
		}
		sc.Name += "-" + strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(suffix))
		sc.ResourceVersion = ""
		err = s.Client.Create(ctx, sc)
	}
	return err
}

func (s *Server) handleCreateSchedule(w http.ResponseWriter, r *http.Request) {
	acc := orgAccessFromContext(r.Context())
	ns, server := r.PathValue("namespace"), r.PathValue("name")

	var req scheduleWrite
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	spec := specFromWrite(server, req)
	if msgs := gameserversv1alpha1.ValidateSchedule(spec); len(msgs) > 0 {
		writeError(w, http.StatusUnprocessableEntity, strings.Join(msgs, "; "))
		return
	}

	existing, err := s.listServerSchedules(r.Context(), ns, server)
	if err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	if len(existing) >= maxSchedulesPerServer {
		writeError(w, http.StatusConflict, fmt.Sprintf("this server already has the maximum of %d schedules", maxSchedulesPerServer))
		return
	}

	var gs gameserversv1alpha1.GameServer
	if err := s.Client.Get(r.Context(), gameServerKey(r), &gs); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	if msg, err := s.scheduleBackupHeadroom(r.Context(), &gs, acc.Org.Slug, spec); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	} else if msg != "" {
		writeError(w, http.StatusUnprocessableEntity, msg)
		return
	}

	sc := &gameserversv1alpha1.GameServerSchedule{
		ObjectMeta: metav1.ObjectMeta{Name: backupplan.ObjectName(server, time.Now()), Namespace: ns},
		Spec:       spec,
	}
	if err := s.createScheduleObject(r.Context(), sc); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toScheduleItem(sc))
}

func (s *Server) handleUpdateSchedule(w http.ResponseWriter, r *http.Request) {
	acc := orgAccessFromContext(r.Context())
	sc, code, err := s.scheduleOfServer(r)
	if err != nil {
		writeError(w, code, err.Error())
		return
	}

	var req scheduleWrite
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	spec := specFromWrite(r.PathValue("name"), req)
	if msgs := gameserversv1alpha1.ValidateSchedule(spec); len(msgs) > 0 {
		writeError(w, http.StatusUnprocessableEntity, strings.Join(msgs, "; "))
		return
	}

	var gs gameserversv1alpha1.GameServer
	if err := s.Client.Get(r.Context(), gameServerKey(r), &gs); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	if msg, err := s.scheduleBackupHeadroom(r.Context(), &gs, acc.Org.Slug, spec); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	} else if msg != "" {
		writeError(w, http.StatusUnprocessableEntity, msg)
		return
	}

	sc.Spec = spec
	if err := s.Client.Update(r.Context(), sc); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toScheduleItem(sc))
}

func (s *Server) handleDeleteSchedule(w http.ResponseWriter, r *http.Request) {
	sc, code, err := s.scheduleOfServer(r)
	if err != nil {
		writeError(w, code, err.Error())
		return
	}
	if err := s.Client.Delete(r.Context(), sc); err != nil && !apierrors.IsNotFound(err) {
		writeError(w, statusFor(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRunSchedule triggers one manual run: the reconciler watches RunNowAnnotation and acts on
// any value it has not already run (status.lastRunNow), so this only needs to set a fresh one.
func (s *Server) handleRunSchedule(w http.ResponseWriter, r *http.Request) {
	sc, code, err := s.scheduleOfServer(r)
	if err != nil {
		writeError(w, code, err.Error())
		return
	}
	patch := client.MergeFrom(sc.DeepCopy())
	if sc.Annotations == nil {
		sc.Annotations = map[string]string{}
	}
	sc.Annotations[gameserversv1alpha1.RunNowAnnotation] = time.Now().UTC().Format(time.RFC3339Nano)
	if err := s.Client.Patch(r.Context(), sc, patch); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
