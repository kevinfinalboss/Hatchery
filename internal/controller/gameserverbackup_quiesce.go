package controller

import (
	"context"
	"fmt"
	"regexp"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

func (r *GameServerBackupReconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// gameTarget returns the backed-up server's Egg backup hooks (nil when the Egg has none or is gone)
// and its Pod when the game container is running (nil otherwise).
func (r *GameServerBackupReconciler) gameTarget(ctx context.Context, bkp *v1alpha1.GameServerBackup) (*v1alpha1.EggBackup, *corev1.Pod, error) {
	var gs v1alpha1.GameServer
	if err := r.Get(ctx, types.NamespacedName{Namespace: bkp.Namespace, Name: bkp.Spec.GameServerRef.Name}, &gs); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	var hooks *v1alpha1.EggBackup
	var egg v1alpha1.Egg
	switch err := r.Get(ctx, types.NamespacedName{Namespace: gs.EggNamespace(), Name: gs.Spec.EggRef.Name}, &egg); {
	case err == nil:
		hooks = egg.Spec.Backup
	case !apierrors.IsNotFound(err):
		return nil, nil, err
	}
	if gs.Status.PodName == "" {
		return hooks, nil, nil
	}
	var pod corev1.Pod
	if err := r.Get(ctx, types.NamespacedName{Namespace: gs.Namespace, Name: gs.Status.PodName}, &pod); err != nil {
		if apierrors.IsNotFound(err) {
			return hooks, nil, nil
		}
		return nil, nil, err
	}
	if !pod.DeletionTimestamp.IsZero() {
		return hooks, nil, nil
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name == serverContainerName && cs.State.Running != nil {
			return hooks, &pod, nil
		}
	}
	return hooks, nil, nil
}

func (r *GameServerBackupReconciler) sendCommands(ctx context.Context, pod *corev1.Pod, cmds []string) error {
	for _, c := range cmds {
		if err := r.Exec(ctx, pod.Namespace, pod.Name, serverContainerName, stdinCommand(c)); err != nil {
			return fmt.Errorf("sending %q: %w", c, err)
		}
	}
	return nil
}

func (r *GameServerBackupReconciler) setCondition(bkp *v1alpha1.GameServerBackup, typ string, status metav1.ConditionStatus, reason, msg string) {
	apimeta.SetStatusCondition(&bkp.Status.Conditions, metav1.Condition{
		Type: typ, Status: status, Reason: reason, Message: msg, ObservedGeneration: bkp.Generation,
	})
}

// quiesce runs the world-save pause before the backup Job. done=false means wait for res.
// The Quiesced condition marks the pause as decided, so it never runs twice.
func (r *GameServerBackupReconciler) quiesce(ctx context.Context, bkp *v1alpha1.GameServerBackup) (bool, ctrl.Result, error) {
	if r.Exec == nil || apimeta.FindStatusCondition(bkp.Status.Conditions, v1alpha1.BackupConditionQuiesced) != nil {
		return true, ctrl.Result{}, nil
	}
	hooks, pod, err := r.gameTarget(ctx, bkp)
	if err != nil {
		return false, ctrl.Result{}, err
	}
	q := bkp.Status.Quiesce
	if q == nil || q.BeforeSentAt == nil {
		if hooks == nil || len(hooks.Before) == 0 || pod == nil {
			return true, ctrl.Result{}, nil
		}
		// Recorded before sending: a restarted operator must never send Before twice, and After is
		// then always attempted.
		sentAt := metav1.NewTime(r.now())
		bkp.Status.Quiesce = &v1alpha1.BackupQuiesceStatus{PodUID: string(pod.UID), BeforeSentAt: &sentAt}
		if err := r.Status().Update(ctx, bkp); err != nil {
			return false, ctrl.Result{}, err
		}
		if err := r.sendCommands(ctx, pod, hooks.Before); err != nil {
			logf.FromContext(ctx).Info("could not pause world saving; backing up anyway", "backup", bkp.Name, "error", err.Error())
			r.setCondition(bkp, v1alpha1.BackupConditionQuiesced, metav1.ConditionFalse, v1alpha1.QuiesceReasonSendFailed, err.Error())
			return true, ctrl.Result{}, r.Status().Update(ctx, bkp)
		}
		q = bkp.Status.Quiesce
	}

	if hooks == nil {
		hooks = &v1alpha1.EggBackup{} // the Egg vanished mid-wait: fall back to the default delay
	}
	matched := false
	if hooks.SavedRegex != "" && pod != nil && string(pod.UID) == q.PodUID && r.Logs != nil {
		if re, err := regexp.Compile(hooks.SavedRegex); err == nil {
			out, err := r.Logs.LogsSince(ctx, pod.Namespace, pod.Name, serverContainerName, q.BeforeSentAt.Time)
			matched = err == nil && re.MatchString(out)
		}
	}
	reason, wait := quiesceWait(hooks, q.BeforeSentAt.Time, r.now(), matched)
	if reason == "" {
		return false, ctrl.Result{RequeueAfter: wait}, nil
	}
	status, msg := metav1.ConditionTrue, ""
	if reason == v1alpha1.QuiesceReasonTimeout {
		status, msg = metav1.ConditionFalse, "the game did not confirm the save in time; backed up anyway"
	}
	r.setCondition(bkp, v1alpha1.BackupConditionQuiesced, status, reason, msg)
	return true, ctrl.Result{}, r.Status().Update(ctx, bkp)
}

// resume sends the After commands once the backup is over (or deleted). pending=true means come
// back after res.
func (r *GameServerBackupReconciler) resume(ctx context.Context, bkp *v1alpha1.GameServerBackup) (bool, ctrl.Result, error) {
	q := bkp.Status.Quiesce
	if r.Exec == nil || q == nil || q.BeforeSentAt == nil || q.ResumedAt != nil {
		return false, ctrl.Result{}, nil
	}
	hooks, pod, err := r.gameTarget(ctx, bkp)
	if err != nil {
		return true, ctrl.Result{}, err
	}
	uid := ""
	if pod != nil {
		uid = string(pod.UID)
	}
	now := metav1.NewTime(r.now())
	switch resumeDecision(q, hooks, uid) {
	case resumeNone:
		return false, ctrl.Result{}, nil
	case resumeSkip:
		reason := v1alpha1.ResumeReasonPodReplaced
		if hooks == nil || len(hooks.After) == 0 {
			reason = v1alpha1.ResumeReasonNothingToSend
		}
		q.ResumedAt = &now
		r.setCondition(bkp, v1alpha1.BackupConditionResumed, metav1.ConditionTrue, reason, "")
	case resumeSend:
		if err := r.sendCommands(ctx, pod, hooks.After); err != nil {
			q.ResumeAttempts++
			if q.ResumeAttempts < resumeMaxAttempts {
				if uerr := r.Status().Update(ctx, bkp); uerr != nil {
					return true, ctrl.Result{}, uerr
				}
				return true, ctrl.Result{RequeueAfter: time.Duration(q.ResumeAttempts) * resumeRetryStep}, nil
			}
			q.ResumedAt = &now
			r.setCondition(bkp, v1alpha1.BackupConditionResumed, metav1.ConditionFalse, v1alpha1.QuiesceReasonSendFailed,
				fmt.Sprintf("world saving may still be paused: restart the server or run the Egg's after commands in the console (%v)", err))
		} else {
			q.ResumedAt = &now
			r.setCondition(bkp, v1alpha1.BackupConditionResumed, metav1.ConditionTrue, v1alpha1.ResumeReasonSent, "")
		}
	}
	return false, ctrl.Result{}, r.Status().Update(ctx, bkp)
}
