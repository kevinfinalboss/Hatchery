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

// GameServerRestoreReconciler reconciles a GameServerRestore object
type GameServerRestoreReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=gameservers.hatchery.io,resources=gameserverrestores,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gameservers.hatchery.io,resources=gameserverrestores/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gameservers.hatchery.io,resources=gameserverrestores/finalizers,verbs=update
// +kubebuilder:rbac:groups=gameservers.hatchery.io,resources=gameserverbackups,verbs=get
// +kubebuilder:rbac:groups=gameservers.hatchery.io,resources=gameservers,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete

// Reconcile drives a GameServerRestore through Pending -> Running ->
// Completed (or Failed): it sets RestoringAnnotation on the target
// GameServer for the duration (the validating webhook is what actually stops
// the target from being started while this is in progress — see
// internal/webhook/v1alpha1), runs a restic restore Job against its data
// PVC, and always clears the annotation on a terminal outcome or on delete.
func (r *GameServerRestoreReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var restore gameserversv1alpha1.GameServerRestore
	if err := r.Get(ctx, req.NamespacedName, &restore); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !restore.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &restore)
	}

	if !controllerutil.ContainsFinalizer(&restore, gameserversv1alpha1.GameServerRestoreFinalizer) {
		controllerutil.AddFinalizer(&restore, gameserversv1alpha1.GameServerRestoreFinalizer)
		if err := r.Update(ctx, &restore); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		return ctrl.Result{}, nil
	}

	switch restore.Status.Phase {
	case "", gameserversv1alpha1.GameServerRestorePhasePending:
		return r.startJob(ctx, &restore)
	case gameserversv1alpha1.GameServerRestorePhaseRunning:
		return r.pollJob(ctx, &restore)
	default:
		log.V(1).Info("restore already in a terminal phase, nothing to do", "phase", restore.Status.Phase)
		return ctrl.Result{}, nil
	}
}

func (r *GameServerRestoreReconciler) startJob(ctx context.Context, restore *gameserversv1alpha1.GameServerRestore) (ctrl.Result, error) {
	var bkp gameserversv1alpha1.GameServerBackup
	if err := r.Get(ctx, types.NamespacedName{Namespace: restore.Namespace, Name: restore.Spec.BackupRef.Name}, &bkp); err != nil {
		if apierrors.IsNotFound(err) {
			return r.completeJob(ctx, restore, gameserversv1alpha1.GameServerRestorePhaseFailed,
				fmt.Sprintf("backup %q no longer exists", restore.Spec.BackupRef.Name))
		}
		return ctrl.Result{}, err
	}

	pvcName := restore.Spec.GameServerRef.Name
	var pvc corev1.PersistentVolumeClaim
	if err := r.Get(ctx, types.NamespacedName{Namespace: restore.Namespace, Name: pvcName}, &pvc); err != nil {
		if apierrors.IsNotFound(err) {
			return r.completeJob(ctx, restore, gameserversv1alpha1.GameServerRestorePhaseFailed,
				fmt.Sprintf("gameserver %q has no data volume", restore.Spec.GameServerRef.Name))
		}
		return ctrl.Result{}, err
	}

	if err := r.setRestoringAnnotation(ctx, restore.Namespace, restore.Spec.GameServerRef.Name, true); err != nil {
		return ctrl.Result{}, fmt.Errorf("locking target gameserver: %w", err)
	}

	job := backup.RestoreJob(restore, bkp.Spec.Destination.S3, bkp.Name, pvcName)
	if err := controllerutil.SetControllerReference(restore, job, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.Create(ctx, job); err != nil && !apierrors.IsAlreadyExists(err) {
		return ctrl.Result{}, fmt.Errorf("creating restore job: %w", err)
	}

	now := metav1.Now()
	restore.Status.Phase = gameserversv1alpha1.GameServerRestorePhaseRunning
	restore.Status.JobName = job.Name
	restore.Status.StartTime = &now
	if err := r.Status().Update(ctx, restore); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: backupJobPollInterval}, nil
}

func (r *GameServerRestoreReconciler) pollJob(ctx context.Context, restore *gameserversv1alpha1.GameServerRestore) (ctrl.Result, error) {
	var job batchv1.Job
	if err := r.Get(ctx, types.NamespacedName{Namespace: restore.Namespace, Name: restore.Status.JobName}, &job); err != nil {
		if apierrors.IsNotFound(err) {
			return r.completeJob(ctx, restore, gameserversv1alpha1.GameServerRestorePhaseFailed, "restore job was deleted before it finished")
		}
		return ctrl.Result{}, err
	}

	switch {
	case job.Status.Succeeded > 0:
		return r.completeJob(ctx, restore, gameserversv1alpha1.GameServerRestorePhaseCompleted, "")
	case job.Status.Failed > 0 && jobIsFinished(&job):
		return r.completeJob(ctx, restore, gameserversv1alpha1.GameServerRestorePhaseFailed, "restore job failed, see its pod logs for details")
	default:
		return ctrl.Result{RequeueAfter: backupJobPollInterval}, nil
	}
}

// completeJob records a terminal phase and releases the lock on the target
// GameServer — from this point on there's nothing left for the finalizer to
// protect, so it's removed here too rather than waiting for the
// GameServerRestore to be deleted.
func (r *GameServerRestoreReconciler) completeJob(ctx context.Context, restore *gameserversv1alpha1.GameServerRestore, phase gameserversv1alpha1.GameServerRestorePhase, message string) (ctrl.Result, error) {
	if err := r.setRestoringAnnotation(ctx, restore.Namespace, restore.Spec.GameServerRef.Name, false); err != nil {
		return ctrl.Result{}, fmt.Errorf("releasing target gameserver lock: %w", err)
	}

	if message != "" {
		logf.FromContext(ctx).Info(message, "restore", restore.Name)
	}

	now := metav1.Now()
	restore.Status.Phase = phase
	restore.Status.CompletionTime = &now
	if err := r.Status().Update(ctx, restore); err != nil {
		return ctrl.Result{}, err
	}

	if controllerutil.ContainsFinalizer(restore, gameserversv1alpha1.GameServerRestoreFinalizer) {
		controllerutil.RemoveFinalizer(restore, gameserversv1alpha1.GameServerRestoreFinalizer)
		if err := r.Update(ctx, restore); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

// reconcileDelete guarantees RestoringAnnotation doesn't outlive a
// GameServerRestore deleted mid-flight — everything else (the Job) is
// cleaned up by Kubernetes garbage collection through the owner reference.
func (r *GameServerRestoreReconciler) reconcileDelete(ctx context.Context, restore *gameserversv1alpha1.GameServerRestore) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(restore, gameserversv1alpha1.GameServerRestoreFinalizer) {
		return ctrl.Result{}, nil
	}
	if err := r.setRestoringAnnotation(ctx, restore.Namespace, restore.Spec.GameServerRef.Name, false); err != nil {
		return ctrl.Result{}, fmt.Errorf("releasing target gameserver lock: %w", err)
	}
	controllerutil.RemoveFinalizer(restore, gameserversv1alpha1.GameServerRestoreFinalizer)
	return ctrl.Result{}, r.Update(ctx, restore)
}

// setRestoringAnnotation sets or clears RestoringAnnotation on the named
// GameServer. A target that no longer exists is not an error here — there's
// nothing left to unlock.
func (r *GameServerRestoreReconciler) setRestoringAnnotation(ctx context.Context, namespace, gameServerName string, restoring bool) error {
	var gs gameserversv1alpha1.GameServer
	if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: gameServerName}, &gs); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	current := gs.Annotations[gameserversv1alpha1.RestoringAnnotation] == "true"
	if current == restoring {
		return nil
	}

	if restoring {
		if gs.Annotations == nil {
			gs.Annotations = map[string]string{}
		}
		gs.Annotations[gameserversv1alpha1.RestoringAnnotation] = "true"
	} else {
		delete(gs.Annotations, gameserversv1alpha1.RestoringAnnotation)
	}
	return r.Update(ctx, &gs)
}

// SetupWithManager sets up the controller with the Manager.
func (r *GameServerRestoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gameserversv1alpha1.GameServerRestore{}).
		Owns(&batchv1.Job{}).
		Named("gameserverrestore").
		Complete(r)
}
