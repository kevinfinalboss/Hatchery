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

package controller

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/backupplan"
)

// PodExecFunc runs cmd in a container of a pod and returns once it exits (an error when it could
// not run or exited non-zero).
type PodExecFunc func(ctx context.Context, namespace, pod, container string, cmd []string) error

// missedRunWindow is how late a fire time may still run. Later than this (the operator was down)
// it is recorded as Skipped instead.
const missedRunWindow = 5 * time.Minute

// GameServerScheduleReconciler runs GameServerSchedules. A run's progress lives in
// status.activeRun, so an operator restart resumes it at the task it was on; between fire times
// and task delays the reconciler just sleeps with RequeueAfter.
type GameServerScheduleReconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	Now     func() time.Time
	Exec    PodExecFunc
	Backups *backupplan.Planner
}

// +kubebuilder:rbac:groups=gameservers.hatchery.io,resources=gameserverschedules,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=gameservers.hatchery.io,resources=gameserverschedules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=pods/exec,verbs=create

func (r *GameServerScheduleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var sched v1alpha1.GameServerSchedule
	if err := r.Get(ctx, req.NamespacedName, &sched); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !sched.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	now := r.Now()
	orig := sched.DeepCopy()

	if msgs := v1alpha1.ValidateSchedule(sched.Spec); len(msgs) > 0 {
		r.setReady(&sched, metav1.ConditionFalse, "InvalidSpec", strings.Join(msgs, "; "))
		return ctrl.Result{}, r.patchStatus(ctx, orig, &sched)
	}

	var gs v1alpha1.GameServer
	if err := r.Get(ctx, types.NamespacedName{Namespace: sched.Namespace, Name: sched.Spec.GameServerRef.Name}, &gs); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		r.setReady(&sched, metav1.ConditionFalse, "GameServerMissing", "game server "+sched.Spec.GameServerRef.Name+" does not exist")
		return ctrl.Result{RequeueAfter: time.Minute}, r.patchStatus(ctx, orig, &sched)
	}
	changed, err := r.ensureOwner(ctx, &sched, &gs)
	if err != nil {
		return ctrl.Result{}, err
	}
	if changed {
		orig = sched.DeepCopy()
	}
	if err := r.rotateBackups(ctx, &sched); err != nil {
		return ctrl.Result{}, err
	}

	if sched.Status.ActiveRun == nil {
		r.maybeStart(&sched, &gs, now)
	}
	if sched.Status.ActiveRun != nil {
		r.advance(ctx, &sched, &gs, now)
	}

	next, _ := v1alpha1.NextRun(sched.Spec, now)
	sched.Status.NextScheduleTime = &metav1.Time{Time: next}
	r.setReady(&sched, metav1.ConditionTrue, "Scheduled", "")
	if err := r.patchStatus(ctx, orig, &sched); err != nil {
		return ctrl.Result{}, err
	}
	wake := next.Sub(now)
	if a := sched.Status.ActiveRun; a != nil {
		if d := a.NextTaskAt.Sub(now); d < wake {
			wake = d
		}
	}
	if wake < time.Second {
		wake = time.Second
	}
	return ctrl.Result{RequeueAfter: wake}, nil
}

// maybeStart starts a run when a manual run was asked for or a fire time is due. A fire time that
// was missed by more than missedRunWindow (operator down) is recorded as Skipped instead: running
// a burst of overdue backups and restarts after an outage is worse than skipping them.
func (r *GameServerScheduleReconciler) maybeStart(s *v1alpha1.GameServerSchedule, gs *v1alpha1.GameServer, now time.Time) {
	if v := s.Annotations[v1alpha1.RunNowAnnotation]; v != "" && v != s.Status.LastRunNow {
		s.Status.LastRunNow = v
		r.start(s, gs, now)
		return
	}
	if s.Spec.Suspend {
		return
	}
	from := s.CreationTimestamp.Time
	if s.Status.LastScheduleTime != nil {
		from = s.Status.LastScheduleTime.Time
	}
	due, err := v1alpha1.NextRun(s.Spec, from)
	if err != nil || due.After(now) {
		return
	}
	latest := due
	for {
		n, err := v1alpha1.NextRun(s.Spec, latest)
		if err != nil || n.After(now) {
			break
		}
		latest = n
	}
	s.Status.LastScheduleTime = &metav1.Time{Time: latest}
	if now.Sub(latest) > missedRunWindow {
		r.finish(s, now, now, v1alpha1.ScheduleRunSkipped, "missed the "+latest.Format(time.RFC3339)+" run (the operator was not running)")
		return
	}
	r.start(s, gs, now)
}

// start begins a run, or records it as Skipped when the server cannot take it.
func (r *GameServerScheduleReconciler) start(s *v1alpha1.GameServerSchedule, gs *v1alpha1.GameServer, now time.Time) {
	switch {
	case gs.Spec.Suspended:
		r.finish(s, now, now, v1alpha1.ScheduleRunSkipped, "server is suspended")
		return
	case gs.Annotations[v1alpha1.RestoringAnnotation] == "true":
		r.finish(s, now, now, v1alpha1.ScheduleRunSkipped, "a restore is in progress")
		return
	case s.Spec.OnlyWhenRunning && gs.Status.Phase != v1alpha1.GameServerPhaseRunning:
		r.finish(s, now, now, v1alpha1.ScheduleRunSkipped, "server is not running")
		return
	}
	s.Status.ActiveRun = &v1alpha1.ActiveScheduleRun{
		ID:         now.UTC().Format(time.RFC3339),
		StartedAt:  metav1.Time{Time: now},
		TaskIndex:  0,
		NextTaskAt: metav1.Time{Time: now.Add(time.Duration(s.Spec.Tasks[0].DelaySeconds) * time.Second)},
	}
}

func (r *GameServerScheduleReconciler) finish(s *v1alpha1.GameServerSchedule, started, now time.Time, result v1alpha1.ScheduleRunResult, msg string) {
	s.Status.ActiveRun = nil
	s.Status.LastRun = &v1alpha1.ScheduleRun{
		StartedAt: metav1.Time{Time: started}, FinishedAt: &metav1.Time{Time: now}, Result: result, Message: msg,
	}
	scheduleRunsTotal.WithLabelValues(string(result)).Inc()
}

// advance runs every task that is due, in order, stopping at the first one still waiting for its
// delay. A failing task ends the run as Failed: later tasks (e.g. a restart after a failed
// backup) do not run.
func (r *GameServerScheduleReconciler) advance(ctx context.Context, s *v1alpha1.GameServerSchedule, gs *v1alpha1.GameServer, now time.Time) {
	for {
		a := s.Status.ActiveRun
		if now.Before(a.NextTaskAt.Time) {
			return
		}
		if int(a.TaskIndex) >= len(s.Spec.Tasks) { // the tasks were shortened mid-run
			r.finish(s, a.StartedAt.Time, now, v1alpha1.ScheduleRunSucceeded, "")
			return
		}
		task := s.Spec.Tasks[a.TaskIndex]
		if err := r.runTask(ctx, s, gs, task, a.ID); err != nil {
			r.finish(s, a.StartedAt.Time, now, v1alpha1.ScheduleRunFailed, fmt.Sprintf("task %d (%s): %v", a.TaskIndex+1, task.Action, err))
			return
		}
		a.TaskIndex++
		if int(a.TaskIndex) >= len(s.Spec.Tasks) {
			r.finish(s, a.StartedAt.Time, now, v1alpha1.ScheduleRunSucceeded, "")
			return
		}
		a.NextTaskAt = metav1.Time{Time: now.Add(time.Duration(s.Spec.Tasks[a.TaskIndex].DelaySeconds) * time.Second)}
	}
}

// runTask performs one task against gs. runID identifies the run, so a retried Backup task does
// not create a second backup.
func (r *GameServerScheduleReconciler) runTask(ctx context.Context, s *v1alpha1.GameServerSchedule, gs *v1alpha1.GameServer, task v1alpha1.ScheduleTask, runID string) error {
	running := gs.Status.Phase == v1alpha1.GameServerPhaseRunning || gs.Status.Phase == v1alpha1.GameServerPhaseStarting
	switch task.Action {
	case v1alpha1.ScheduleActionCommand:
		if !running || gs.Status.PodName == "" {
			return fmt.Errorf("server is not running")
		}
		if r.Exec == nil {
			return errors.New("running commands is not configured in the operator")
		}
		return r.Exec(ctx, gs.Namespace, gs.Status.PodName, serverContainerName, stdinCommand(task.Command))
	case v1alpha1.ScheduleActionRestart:
		if !running {
			return nil // nothing to restart; not a failure
		}
		patch := client.MergeFrom(gs.DeepCopy())
		if gs.Annotations == nil {
			gs.Annotations = map[string]string{}
		}
		gs.Annotations[v1alpha1.RestartAnnotation] = r.Now().UTC().Format(time.RFC3339Nano)
		return r.Patch(ctx, gs, patch)
	case v1alpha1.ScheduleActionStart, v1alpha1.ScheduleActionStop:
		if gs.Spec.Suspended {
			return fmt.Errorf("server is suspended")
		}
		if gs.Annotations[v1alpha1.RestoringAnnotation] == "true" {
			return fmt.Errorf("a restore is in progress")
		}
		want := v1alpha1.GameServerStateRunning
		if task.Action == v1alpha1.ScheduleActionStop {
			want = v1alpha1.GameServerStateStopped
		}
		if gs.Spec.State == want {
			return nil
		}
		patch := client.MergeFrom(gs.DeepCopy())
		gs.Spec.State = want
		return r.Patch(ctx, gs, patch)
	case v1alpha1.ScheduleActionBackup:
		return r.backup(ctx, s, gs, runID)
	}
	return fmt.Errorf("unknown action %q", task.Action)
}

func (r *GameServerScheduleReconciler) backup(ctx context.Context, s *v1alpha1.GameServerSchedule, gs *v1alpha1.GameServer, runID string) error {
	if r.Backups == nil {
		return errors.New("backups are not configured in the operator")
	}
	var existing v1alpha1.GameServerBackupList
	if err := r.List(ctx, &existing, client.InNamespace(s.Namespace), client.MatchingLabels{v1alpha1.ScheduleLabel: s.Name}); err != nil {
		return err
	}
	for i := range existing.Items {
		if existing.Items[i].Annotations[v1alpha1.ScheduleRunAnnotation] == runID {
			return nil // this run already made its backup (retried after a status write failed)
		}
	}
	var ns corev1.Namespace
	if err := r.Get(ctx, types.NamespacedName{Name: s.Namespace}, &ns); err != nil {
		return err
	}
	bkp, err := r.Backups.Plan(ctx, gs, ns.Labels[v1alpha1.LabelTenant])
	if err != nil {
		return err // a *backupplan.ConflictError carries the same text as the Panel's 409
	}
	if bkp.Labels == nil {
		bkp.Labels = map[string]string{}
	}
	bkp.Labels[v1alpha1.ScheduleLabel] = s.Name
	if bkp.Annotations == nil {
		bkp.Annotations = map[string]string{}
	}
	bkp.Annotations[v1alpha1.ScheduleRunAnnotation] = runID
	return r.Backups.Create(ctx, bkp)
}

// rotateBackups keeps the newest keepLast Completed backups this schedule made. It only acts once
// the schedule's newest backup is Completed, so a new backup that fails never costs a good old one.
// Manual backups (no ScheduleLabel) and failed ones are left alone.
func (r *GameServerScheduleReconciler) rotateBackups(ctx context.Context, s *v1alpha1.GameServerSchedule) error {
	keep := int32(0)
	for _, t := range s.Spec.Tasks {
		if t.Action == v1alpha1.ScheduleActionBackup && t.KeepLast > keep {
			keep = t.KeepLast
		}
	}
	if keep == 0 {
		return nil
	}
	var list v1alpha1.GameServerBackupList
	if err := r.List(ctx, &list, client.InNamespace(s.Namespace), client.MatchingLabels{v1alpha1.ScheduleLabel: s.Name}); err != nil {
		return err
	}
	items := list.Items
	// Newest first. CreationTimestamp has second resolution; the name (ObjectName: server plus the
	// creation time in base36) breaks ties in the same order.
	sort.Slice(items, func(i, j int) bool {
		ti, tj := items[i].CreationTimestamp.Time, items[j].CreationTimestamp.Time
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return items[i].Name > items[j].Name
	})
	if len(items) == 0 || items[0].Status.Phase != v1alpha1.GameServerBackupPhaseCompleted {
		return nil
	}
	var completed int32
	for i := range items {
		b := &items[i]
		if b.Status.Phase != v1alpha1.GameServerBackupPhaseCompleted || !b.DeletionTimestamp.IsZero() {
			continue
		}
		completed++
		if completed > keep {
			if err := r.Delete(ctx, b); client.IgnoreNotFound(err) != nil {
				return err
			}
		}
	}
	return nil
}

func (r *GameServerScheduleReconciler) setReady(s *v1alpha1.GameServerSchedule, status metav1.ConditionStatus, reason, msg string) {
	apimeta.SetStatusCondition(&s.Status.Conditions, metav1.Condition{
		Type: "Ready", Status: status, Reason: reason, Message: msg, ObservedGeneration: s.Generation,
	})
}

func (r *GameServerScheduleReconciler) patchStatus(ctx context.Context, orig, s *v1alpha1.GameServerSchedule) error {
	return r.Status().Patch(ctx, s, client.MergeFrom(orig))
}

// ensureOwner makes the GameServer the schedule's controller, so deleting the server deletes its
// schedules. It reports whether it had to update the schedule.
func (r *GameServerScheduleReconciler) ensureOwner(ctx context.Context, s *v1alpha1.GameServerSchedule, gs *v1alpha1.GameServer) (bool, error) {
	if metav1.IsControlledBy(s, gs) {
		return false, nil
	}
	if err := controllerutil.SetControllerReference(gs, s, r.Scheme); err != nil {
		return false, err
	}
	if err := r.Update(ctx, s); err != nil {
		return false, err
	}
	return true, nil
}

// SetupWithManager sets up the controller with the Manager. Backups carry the schedule's name in
// ScheduleLabel (the schedule is not their owner), so a backup finishing wakes the schedule up for
// its rotation.
func (r *GameServerScheduleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.GameServerSchedule{}).
		Watches(&v1alpha1.GameServerBackup{}, handler.EnqueueRequestsFromMapFunc(
			func(_ context.Context, obj client.Object) []reconcile.Request {
				name := obj.GetLabels()[v1alpha1.ScheduleLabel]
				if name == "" {
					return nil
				}
				return []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: obj.GetNamespace(), Name: name}}}
			})).
		Named("gameserverschedule").
		Complete(r)
}
