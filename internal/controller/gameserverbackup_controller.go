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
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/backup"
)

// backupJobPollInterval is how often the controller re-checks a Job that's
// still running. There's no watch-driven wakeup for that (Owns(&batchv1.Job{})
// below already covers the event-driven case); this is only the fallback for
// however long a restic run itself takes between Job status updates.
const backupJobPollInterval = 5 * time.Second

// GameServerBackupReconciler reconciles a GameServerBackup object
type GameServerBackupReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=gameservers.hatchery.io,resources=gameserverbackups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gameservers.hatchery.io,resources=gameserverbackups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gameservers.hatchery.io,resources=gameserverbackups/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete

// Reconcile drives a GameServerBackup through Pending -> Running -> Completed
// (or Failed) by running a restic Job (see internal/backup) against the
// target GameServer's data PVC, and prunes the remote snapshot before letting
// the object actually delete (see reconcileDelete) — Kubernetes garbage
// collection has no idea an S3 bucket exists.
func (r *GameServerBackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var bkp gameserversv1alpha1.GameServerBackup
	if err := r.Get(ctx, req.NamespacedName, &bkp); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !bkp.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &bkp)
	}

	if !controllerutil.ContainsFinalizer(&bkp, gameserversv1alpha1.GameServerBackupFinalizer) {
		controllerutil.AddFinalizer(&bkp, gameserversv1alpha1.GameServerBackupFinalizer)
		if err := r.Update(ctx, &bkp); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		return ctrl.Result{}, nil
	}

	switch bkp.Status.Phase {
	case "", gameserversv1alpha1.GameServerBackupPhasePending:
		return r.startJob(ctx, &bkp)
	case gameserversv1alpha1.GameServerBackupPhaseRunning:
		return r.pollJob(ctx, &bkp)
	default:
		log.V(1).Info("backup already in a terminal phase", "phase", bkp.Status.Phase)
		return r.enforceRetention(ctx, &bkp)
	}
}

// maxRetentionWait caps how long the controller sleeps before looking at a backup's expiry again.
const maxRetentionWait = time.Hour

// enforceRetention deletes a finished backup once its expires-at annotation has passed (the
// finalizer then prunes the snapshot), and otherwise asks to be woken up around the expiry. A backup
// without a valid annotation is kept for good.
func (r *GameServerBackupReconciler) enforceRetention(ctx context.Context, bkp *gameserversv1alpha1.GameServerBackup) (ctrl.Result, error) {
	raw := bkp.Annotations[gameserversv1alpha1.BackupExpiresAtAnnotation]
	if raw == "" {
		return ctrl.Result{}, nil
	}
	expiresAt, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		logf.FromContext(ctx).Info("ignoring an unparsable expires-at annotation", "value", raw)
		return ctrl.Result{}, nil
	}
	if remaining := time.Until(expiresAt); remaining > 0 {
		return ctrl.Result{RequeueAfter: min(remaining+time.Second, maxRetentionWait)}, nil
	}
	logf.FromContext(ctx).Info("deleting an expired backup", "backup", bkp.Name, "expiredAt", raw)
	return ctrl.Result{}, client.IgnoreNotFound(r.Delete(ctx, bkp))
}

// startJob validates the target GameServer's PVC exists and creates the
// backup Job.
func (r *GameServerBackupReconciler) startJob(ctx context.Context, bkp *gameserversv1alpha1.GameServerBackup) (ctrl.Result, error) {
	pvcName := bkp.Spec.GameServerRef.Name
	var pvc corev1.PersistentVolumeClaim
	if err := r.Get(ctx, types.NamespacedName{Namespace: bkp.Namespace, Name: pvcName}, &pvc); err != nil {
		if apierrors.IsNotFound(err) {
			return r.completeJob(ctx, bkp, gameserversv1alpha1.GameServerBackupPhaseFailed,
				fmt.Sprintf("gameserver %q has no data volume (has it been created yet?)", bkp.Spec.GameServerRef.Name))
		}
		return ctrl.Result{}, err
	}

	job := backup.BackupJob(bkp, pvcName)
	if err := controllerutil.SetControllerReference(bkp, job, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.Create(ctx, job); err != nil && !apierrors.IsAlreadyExists(err) {
		return ctrl.Result{}, fmt.Errorf("creating backup job: %w", err)
	}

	now := metav1.Now()
	bkp.Status.Phase = gameserversv1alpha1.GameServerBackupPhaseRunning
	bkp.Status.JobName = job.Name
	bkp.Status.StartTime = &now
	if err := r.Status().Update(ctx, bkp); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: backupJobPollInterval}, nil
}

// pollJob checks the backup Job's status and advances the phase once it
// reaches a terminal state.
func (r *GameServerBackupReconciler) pollJob(ctx context.Context, bkp *gameserversv1alpha1.GameServerBackup) (ctrl.Result, error) {
	var job batchv1.Job
	if err := r.Get(ctx, types.NamespacedName{Namespace: bkp.Namespace, Name: bkp.Status.JobName}, &job); err != nil {
		if apierrors.IsNotFound(err) {
			// Job vanished out from under us; restart from scratch.
			return r.completeJob(ctx, bkp, gameserversv1alpha1.GameServerBackupPhaseFailed, "backup job was deleted before it finished")
		}
		return ctrl.Result{}, err
	}

	switch {
	case job.Status.Succeeded > 0:
		return r.completeJob(ctx, bkp, gameserversv1alpha1.GameServerBackupPhaseCompleted, "")
	case job.Status.Failed > 0 && jobIsFinished(&job):
		return r.completeJob(ctx, bkp, gameserversv1alpha1.GameServerBackupPhaseFailed, "backup job failed, see its pod logs for details")
	default:
		return ctrl.Result{RequeueAfter: backupJobPollInterval}, nil
	}
}

// jobIsFinished reports whether a Job has given up retrying (backoffLimit
// exceeded) rather than just failed a single attempt it may still retry.
func jobIsFinished(job *batchv1.Job) bool {
	for _, c := range job.Status.Conditions {
		if (c.Type == batchv1.JobFailed || c.Type == batchv1.JobComplete) && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return job.Status.Failed > 0 && job.Spec.BackoffLimit != nil && job.Status.Failed > *job.Spec.BackoffLimit
}

func (r *GameServerBackupReconciler) completeJob(ctx context.Context, bkp *gameserversv1alpha1.GameServerBackup, phase gameserversv1alpha1.GameServerBackupPhase, message string) (ctrl.Result, error) {
	now := metav1.Now()
	bkp.Status.Phase = phase
	bkp.Status.CompletionTime = &now
	if message != "" {
		logf.FromContext(ctx).Info(message, "backup", bkp.Name)
	}
	return ctrl.Result{}, r.Status().Update(ctx, bkp)
}

// reconcileDelete prunes the remote snapshot (if a backup ever actually
// completed — nothing was durably stored otherwise) via a cleanup Job before
// releasing the finalizer.
func (r *GameServerBackupReconciler) reconcileDelete(ctx context.Context, bkp *gameserversv1alpha1.GameServerBackup) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(bkp, gameserversv1alpha1.GameServerBackupFinalizer) {
		return ctrl.Result{}, nil
	}

	if bkp.Status.Phase != gameserversv1alpha1.GameServerBackupPhaseCompleted &&
		bkp.Status.Phase != gameserversv1alpha1.GameServerBackupPhaseDeleting {
		// Never completed: nothing meaningful was durably stored under this
		// backup's tag, so there's nothing to prune remotely.
		controllerutil.RemoveFinalizer(bkp, gameserversv1alpha1.GameServerBackupFinalizer)
		return ctrl.Result{}, r.Update(ctx, bkp)
	}

	var job batchv1.Job
	err := r.Get(ctx, types.NamespacedName{Namespace: bkp.Namespace, Name: backup.CleanupJobName(bkp.Name)}, &job)
	switch {
	case apierrors.IsNotFound(err):
		cleanupJob := backup.CleanupJob(bkp)
		if err := controllerutil.SetControllerReference(bkp, cleanupJob, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, cleanupJob); err != nil && !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, fmt.Errorf("creating cleanup job: %w", err)
		}
		bkp.Status.Phase = gameserversv1alpha1.GameServerBackupPhaseDeleting
		if err := r.Status().Update(ctx, bkp); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: backupJobPollInterval}, nil
	case err != nil:
		return ctrl.Result{}, err
	case job.Status.Succeeded > 0, job.Status.Failed > 0 && jobIsFinished(&job):
		// Whether the prune succeeded or exhausted its retries, we don't
		// block deletion forever on remote cleanup — see the finalizer's doc
		// comment on GameServerBackupFinalizer.
		controllerutil.RemoveFinalizer(bkp, gameserversv1alpha1.GameServerBackupFinalizer)
		return ctrl.Result{}, r.Update(ctx, bkp)
	default:
		return ctrl.Result{RequeueAfter: backupJobPollInterval}, nil
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *GameServerBackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gameserversv1alpha1.GameServerBackup{}).
		Owns(&batchv1.Job{}).
		Named("gameserverbackup").
		Complete(r)
}
