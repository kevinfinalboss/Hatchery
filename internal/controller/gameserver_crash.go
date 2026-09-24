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
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

// serverTerminated returns how the game container ended, or nil while it has not.
func serverTerminated(pod *corev1.Pod) *corev1.ContainerStateTerminated {
	for _, c := range pod.Status.ContainerStatuses {
		if c.Name == serverContainerName {
			return c.State.Terminated
		}
	}
	return nil
}

// crashedPod returns the game container's termination when it ended on its own: the server is meant to
// run, and nobody is stopping, suspending or restarting it. Those paths end the container too, and
// already have their own handling.
func crashedPod(gs *gameserversv1alpha1.GameServer, pod *corev1.Pod) *corev1.ContainerStateTerminated {
	if pod == nil || !pod.DeletionTimestamp.IsZero() || gs.Spec.State != gameserversv1alpha1.GameServerStateRunning ||
		gs.Spec.Suspended || restartRequested(gs, pod) {
		return nil
	}
	return serverTerminated(pod)
}

// reconcileCrash handles a game process that ended on its own (see decideCrash). It returns how long to
// wait before the crashed server may be restarted, or 0.
func (r *GameServerReconciler) reconcileCrash(ctx context.Context, gs *gameserversv1alpha1.GameServer, pod *corev1.Pod) (time.Duration, error) {
	term := crashedPod(gs, pod)
	if term == nil {
		return 0, nil
	}
	log := logf.FromContext(ctx)
	policy := r.CrashPolicy.orDefault()
	d := decideCrash(term, gs.AutoRestartEnabled(), gs.Status.RecentCrashes, policy)

	if d.action == crashCleanExit {
		log.Info("the game exited with code 0 on its own; stopping the server")
		if err := r.stopAfterExit(ctx, gs, pod); err != nil {
			return 0, err
		}
		gameServerCrashesTotal.WithLabelValues("clean_exit").Inc()
		return 0, nil
	}

	crash := gameserversv1alpha1.GameServerCrash{At: term.FinishedAt, ExitCode: term.ExitCode, Reason: term.Reason, OOMKilled: isOOMKilled(term)}
	if gs.Status.LastCrash == nil || gs.Status.LastCrash.At.Unix() != crash.At.Unix() {
		// First time this crash is seen: keep the console before the Pod is deleted.
		if err := r.saveCrashLog(ctx, gs, pod, crash); err != nil {
			return 0, err
		}
	}

	before := gs.Status.DeepCopy()
	gs.Status.LastCrash = &crash
	gs.Status.RecentCrashes = d.recent
	if d.action == crashGiveUp {
		apimeta.SetStatusCondition(&gs.Status.Conditions, metav1.Condition{
			Type: gameserversv1alpha1.ConditionCrashed, Status: metav1.ConditionTrue, Reason: d.reason,
			Message: crashMessage(d, term, policy), ObservedGeneration: gs.Generation,
		})
	}
	if !equality.Semantic.DeepEqual(before, &gs.Status) {
		if err := r.Status().Update(ctx, gs); err != nil {
			return 0, err
		}
	}

	if d.action == crashGiveUp {
		log.Info("giving up on a crashing server", "reason", d.reason, "exitCode", term.ExitCode, "crashes", len(d.recent))
		if err := r.stopAfterExit(ctx, gs, pod); err != nil {
			return 0, err
		}
		gameServerCrashesTotal.WithLabelValues("gave_up").Inc()
		return 0, nil
	}

	if wait := time.Until(d.restartAt); wait > 0 {
		return wait, nil
	}
	log.Info("restarting a crashed server", "exitCode", term.ExitCode, "crashes", len(d.recent))
	if err := r.Delete(ctx, pod); err != nil && !apierrors.IsNotFound(err) {
		return 0, err
	}
	gameServerCrashesTotal.WithLabelValues("restarted").Inc()
	return 0, nil
}

// stopAfterExit leaves the server stopped: spec.state goes to Stopped (so Start works as usual) and the
// dead Pod is deleted, which also frees the org's quota.
func (r *GameServerReconciler) stopAfterExit(ctx context.Context, gs *gameserversv1alpha1.GameServer, pod *corev1.Pod) error {
	gs.Spec.State = gameserversv1alpha1.GameServerStateStopped
	if err := r.Update(ctx, gs); err != nil {
		return err
	}
	if err := r.Delete(ctx, pod); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

// saveCrashLog writes the crashed console's tail to the server's crash log ConfigMap, replacing the
// previous crash. A console that cannot be read is saved empty: it must never keep a server from restarting.
func (r *GameServerReconciler) saveCrashLog(ctx context.Context, gs *gameserversv1alpha1.GameServer, pod *corev1.Pod, crash gameserversv1alpha1.GameServerCrash) error {
	out := ""
	if r.LogReader != nil {
		s, err := r.LogReader.TailLogs(ctx, pod.Namespace, pod.Name, serverContainerName, crashLogLines)
		if err != nil {
			logf.FromContext(ctx).Info("could not read the crashed server's console", "error", err.Error())
		} else {
			out = trimCrashLog(s)
		}
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: gameserversv1alpha1.CrashLogConfigMapName(gs.Name), Namespace: gs.Namespace},
		Data: map[string]string{
			gameserversv1alpha1.CrashLogKeyLog:       out,
			gameserversv1alpha1.CrashLogKeyAt:        crash.At.UTC().Format(time.RFC3339),
			gameserversv1alpha1.CrashLogKeyExitCode:  strconv.Itoa(int(crash.ExitCode)),
			gameserversv1alpha1.CrashLogKeyReason:    crash.Reason,
			gameserversv1alpha1.CrashLogKeyOOMKilled: strconv.FormatBool(crash.OOMKilled),
		},
	}
	if err := controllerutil.SetControllerReference(gs, cm, r.Scheme); err != nil {
		return err
	}
	err := r.Create(ctx, cm)
	if apierrors.IsAlreadyExists(err) {
		// No resourceVersion: an unconditional replace. The operator is the only writer, and this way it
		// never reads ConfigMaps (which would start a cluster-wide ConfigMap informer).
		err = r.Update(ctx, cm)
	}
	return err
}
