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
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/sftpagent"
)

// dataVolumeName/dataMountPath name the volume the "server" container and the
// sftp-agent sidecar share for the server's data directory. Defined once in
// internal/sftpagent so the Panel API's standalone maintenance Pod (see
// internal/panelapi) can reuse the exact same names without risking drift.
const (
	dataVolumeName = sftpagent.DefaultDataVolumeName
	dataMountPath  = sftpagent.DefaultDataMountPath
)

// GameServerReconciler reconciles a GameServer object
type GameServerReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// SFTPAgentImage is the container image used for the sftp-agent sidecar
	// injected into every Running GameServer's Pod (see internal/sftpagent).
	SFTPAgentImage string
}

// +kubebuilder:rbac:groups=gameservers.hatchery.io,resources=gameservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gameservers.hatchery.io,resources=gameservers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gameservers.hatchery.io,resources=gameservers/finalizers,verbs=update
// +kubebuilder:rbac:groups=gameservers.hatchery.io,resources=eggs,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile moves the cluster state for a single GameServer towards its desired
// state: an Egg-derived Pod, a Service exposing the Egg's ports, and a PVC
// backing the server's data directory. Pod/Service/PVC cleanup on delete is
// handled by Kubernetes garbage collection through owner references; the
// GameServerFinalizer exists for cleanup Kubernetes can't do on its own (today:
// dropping this object's Prometheus series; later: releasing an external
// allocation, telling the Panel API).
func (r *GameServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, reterr error) {
	log := logf.FromContext(ctx)

	start := time.Now()
	defer func() {
		outcome := "success"
		if reterr != nil {
			outcome = "error"
		}
		gameServerReconcileTotal.WithLabelValues(outcome).Inc()
		gameServerReconcileDuration.WithLabelValues(outcome).Observe(time.Since(start).Seconds())
	}()

	var gs gameserversv1alpha1.GameServer
	if err := r.Get(ctx, req.NamespacedName, &gs); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !gs.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &gs)
	}

	if !controllerutil.ContainsFinalizer(&gs, gameserversv1alpha1.GameServerFinalizer) {
		controllerutil.AddFinalizer(&gs, gameserversv1alpha1.GameServerFinalizer)
		if err := r.Update(ctx, &gs); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		return ctrl.Result{}, nil
	}

	// The sftp-agent secret is independent of the Egg (and of whether the
	// server is Running or Stopped): the Panel API needs it to mint sessions
	// for the on-demand maintenance Pod even while the server itself isn't
	// running, so it's reconciled unconditionally rather than alongside the
	// Pod.
	if err := r.reconcileSecret(ctx, &gs); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling sftp secret: %w", err)
	}

	var egg gameserversv1alpha1.Egg
	eggKey := types.NamespacedName{Namespace: gs.Namespace, Name: gs.Spec.EggRef.Name}
	if err := r.Get(ctx, eggKey, &egg); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("referenced Egg not found, waiting", "egg", eggKey.Name)
			return r.setPhase(ctx, &gs, gameserversv1alpha1.GameServerPhaseFailed)
		}
		return ctrl.Result{}, err
	}

	if err := r.reconcilePVC(ctx, &gs); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling pvc: %w", err)
	}

	if err := r.reconcileService(ctx, &gs, &egg); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling service: %w", err)
	}

	pod, err := r.reconcilePod(ctx, &gs, &egg)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling pod: %w", err)
	}

	return r.updateStatus(ctx, &gs, pod)
}

// reconcileDelete runs the finalizer's cleanup and then releases it. Pod,
// Service and PVC are already gone or going away via owner references by the
// time this runs; this is only for state Kubernetes GC doesn't know about.
func (r *GameServerReconciler) reconcileDelete(ctx context.Context, gs *gameserversv1alpha1.GameServer) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(gs, gameserversv1alpha1.GameServerFinalizer) {
		return ctrl.Result{}, nil
	}

	removeGameServerMetrics(gs.Namespace, gs.Name)

	controllerutil.RemoveFinalizer(gs, gameserversv1alpha1.GameServerFinalizer)
	if err := r.Update(ctx, gs); err != nil {
		return ctrl.Result{}, fmt.Errorf("removing finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

// reconcilePVC ensures the data PVC backing this GameServer exists. Storage is
// immutable in the MVP, so this only creates it once and never patches it.
func (r *GameServerReconciler) reconcilePVC(ctx context.Context, gs *gameserversv1alpha1.GameServer) error {
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: gs.Name, Namespace: gs.Namespace}}
	err := r.Get(ctx, client.ObjectKeyFromObject(pvc), pvc)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	size, err := resource.ParseQuantity(gs.Spec.Storage.Size)
	if err != nil {
		return fmt.Errorf("parsing spec.storage.size %q: %w", gs.Spec.Storage.Size, err)
	}

	pvc = &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gs.Name,
			Namespace: gs.Namespace,
			Labels:    gameServerLabels(gs.Name),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: size},
			},
			StorageClassName: gs.Spec.Storage.StorageClassName,
		},
	}
	if err := controllerutil.SetControllerReference(gs, pvc, r.Scheme); err != nil {
		return err
	}
	return r.Create(ctx, pvc)
}

// reconcileSecret ensures the per-GameServer HMAC secret the sftp-agent uses
// to verify session tokens exists. It's generated once and never rotated
// automatically in the MVP — rotating it would invalidate every
// already-issued token, which is fine for an expired one but not something
// to do implicitly on every reconcile.
func (r *GameServerReconciler) reconcileSecret(ctx context.Context, gs *gameserversv1alpha1.GameServer) error {
	name := sftpagent.SecretName(gs.Name)
	var existing corev1.Secret
	err := r.Get(ctx, types.NamespacedName{Namespace: gs.Namespace, Name: name}, &existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return fmt.Errorf("generating sftp hmac key: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: gs.Namespace,
			Labels:    gameServerLabels(gs.Name),
		},
		Data: sftpagent.SecretData(key),
	}
	if err := controllerutil.SetControllerReference(gs, secret, r.Scheme); err != nil {
		return err
	}
	return r.Create(ctx, secret)
}

// reconcileService ensures a ClusterIP Service exposes the Egg's declared
// ports plus the sftp-agent's port — the latter always present so the same
// Service reaches SFTP whether it's currently backed by the sidecar (server
// Running) or the standalone maintenance Pod the Panel API creates on demand
// when it's Stopped (see internal/sftpagent and the Panel API's SFTP session
// handler); both use the same GameServer labels as their selector. How game
// ports specifically get exposed to players (NodePort, LoadBalancer, a shared
// ingress-style proxy) is a decision for a later milestone, not part of this
// scaffolding.
func (r *GameServerReconciler) reconcileService(ctx context.Context, gs *gameserversv1alpha1.GameServer, egg *gameserversv1alpha1.Egg) error {
	ports := append(servicePorts(egg), sftpagent.ServicePort())

	desired := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gs.Name,
			Namespace: gs.Namespace,
			Labels:    gameServerLabels(gs.Name),
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, desired, func() error {
		desired.Spec.Selector = gameServerLabels(gs.Name)
		desired.Spec.Ports = ports
		return controllerutil.SetControllerReference(gs, desired, r.Scheme)
	})
	return err
}

// reconcilePod drives the server Pod towards the desired state: created when
// State is Running and absent; deleted when State is Stopped and present.
// It returns the current Pod, or nil if none should exist.
func (r *GameServerReconciler) reconcilePod(ctx context.Context, gs *gameserversv1alpha1.GameServer, egg *gameserversv1alpha1.Egg) (*corev1.Pod, error) {
	var pod corev1.Pod
	err := r.Get(ctx, types.NamespacedName{Namespace: gs.Namespace, Name: gs.Name}, &pod)
	exists := err == nil
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, err
	}

	if gs.Spec.State == gameserversv1alpha1.GameServerStateStopped {
		if exists {
			return nil, r.Delete(ctx, &pod)
		}
		return nil, nil
	}

	if exists {
		return &pod, nil
	}

	desired, err := buildPod(gs, egg, r.SFTPAgentImage)
	if err != nil {
		return nil, err
	}
	if err := controllerutil.SetControllerReference(gs, desired, r.Scheme); err != nil {
		return nil, err
	}
	if err := r.Create(ctx, desired); err != nil {
		return nil, err
	}
	return desired, nil
}

// updateStatus recomputes GameServerStatus from the live Pod (if any) and
// patches it when it drifted from what's stored.
func (r *GameServerReconciler) updateStatus(ctx context.Context, gs *gameserversv1alpha1.GameServer, pod *corev1.Pod) (ctrl.Result, error) {
	phase := gameserversv1alpha1.GameServerPhasePending
	podName := ""

	switch {
	case gs.Spec.State == gameserversv1alpha1.GameServerStateStopped && pod == nil:
		phase = gameserversv1alpha1.GameServerPhaseStopped
	case pod == nil:
		phase = gameserversv1alpha1.GameServerPhasePending
	case pod.Status.Phase == corev1.PodRunning:
		phase = gameserversv1alpha1.GameServerPhaseRunning
		podName = pod.Name
	case pod.Status.Phase == corev1.PodFailed:
		phase = gameserversv1alpha1.GameServerPhaseFailed
		podName = pod.Name
	default:
		phase = gameserversv1alpha1.GameServerPhasePending
		podName = pod.Name
	}

	if gs.Status.Phase == phase && gs.Status.PodName == podName && gs.Status.ObservedGeneration == gs.Generation {
		return ctrl.Result{}, nil
	}

	oldPhase := gs.Status.Phase
	gs.Status.Phase = phase
	gs.Status.PodName = podName
	gs.Status.ObservedGeneration = gs.Generation
	if err := r.Status().Update(ctx, gs); err != nil {
		return ctrl.Result{}, err
	}
	recordGameServerPhase(gs, oldPhase, phase)
	return ctrl.Result{}, nil
}

func (r *GameServerReconciler) setPhase(ctx context.Context, gs *gameserversv1alpha1.GameServer, phase gameserversv1alpha1.GameServerPhase) (ctrl.Result, error) {
	if gs.Status.Phase == phase {
		return ctrl.Result{}, nil
	}
	oldPhase := gs.Status.Phase
	gs.Status.Phase = phase
	if err := r.Status().Update(ctx, gs); err != nil {
		return ctrl.Result{}, err
	}
	recordGameServerPhase(gs, oldPhase, phase)
	return ctrl.Result{}, nil
}

// gameServerLabels is a thin local alias for gameserversv1alpha1.GameServerLabels
// — kept so the rest of this file doesn't need the longer package-qualified
// name at every call site.
func gameServerLabels(name string) map[string]string {
	return gameserversv1alpha1.GameServerLabels(name)
}

func servicePorts(egg *gameserversv1alpha1.Egg) []corev1.ServicePort {
	ports := make([]corev1.ServicePort, 0, len(egg.Spec.Ports))
	for _, p := range egg.Spec.Ports {
		protocol := p.Protocol
		if protocol == "" {
			protocol = corev1.ProtocolTCP
		}
		ports = append(ports, corev1.ServicePort{
			Name:       p.Name,
			Port:       p.ContainerPort,
			TargetPort: intstr.FromInt32(p.ContainerPort),
			Protocol:   protocol,
		})
	}
	return ports
}

// buildPod renders the desired Pod for a GameServer from its Egg: resolves
// variables, substitutes them into the Egg's start command, wires up the
// install initContainer when the Egg declares one, and adds the sftp-agent
// sidecar (see internal/sftpagent) sharing the same data volume.
func buildPod(gs *gameserversv1alpha1.GameServer, egg *gameserversv1alpha1.Egg, sftpAgentImage string) (*corev1.Pod, error) {
	vars, err := resolveVariables(egg, gs)
	if err != nil {
		return nil, err
	}

	env := make([]corev1.EnvVar, 0, len(vars))
	for name, value := range vars {
		env = append(env, corev1.EnvVar{Name: name, Value: value})
	}

	containerPorts := make([]corev1.ContainerPort, 0, len(egg.Spec.Ports))
	for _, p := range egg.Spec.Ports {
		protocol := p.Protocol
		if protocol == "" {
			protocol = corev1.ProtocolTCP
		}
		containerPorts = append(containerPorts, corev1.ContainerPort{
			Name:          p.Name,
			ContainerPort: p.ContainerPort,
			Protocol:      protocol,
		})
	}

	secretName := sftpagent.SecretName(gs.Name)
	volumes := []corev1.Volume{
		{
			Name: dataVolumeName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: gs.Name},
			},
		},
		sftpagent.SecretVolume(secretName),
	}
	mounts := []corev1.VolumeMount{{Name: dataVolumeName, MountPath: dataMountPath}}

	var initContainers []corev1.Container
	if egg.Spec.Install != nil {
		image := egg.Spec.Install.Image
		if image == "" {
			image = egg.Spec.Image
		}
		initContainers = append(initContainers, corev1.Container{
			Name:         "install",
			Image:        image,
			WorkingDir:   dataMountPath,
			Command:      installCommand(egg.Spec.Install),
			Env:          env,
			VolumeMounts: mounts,
		})
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gs.Name,
			Namespace: gs.Namespace,
			Labels:    gameServerLabels(gs.Name),
		},
		Spec: corev1.PodSpec{
			RestartPolicy:  corev1.RestartPolicyNever,
			InitContainers: initContainers,
			// fsGroup lets the sftp-agent sidecar and the "server" container
			// share files on the data volume regardless of which uid the
			// Egg's image runs the server as — see sftpagent.SharedFSGroup.
			SecurityContext: &corev1.PodSecurityContext{FSGroup: sftpagent.FSGroupPtr()},
			Containers: []corev1.Container{{
				Name:  "server",
				Image: egg.Spec.Image,
				// "exec" makes a POSIX shell replace itself with the target
				// process instead of forking it, so the server binary — not
				// /bin/sh — ends up as the container's PID 1. That matters for
				// two things the Panel API depends on: `kubectl attach`-style
				// console access writes to PID 1's stdin, and StopSignal is
				// delivered to PID 1. Without "exec" both would silently hit
				// the shell instead of the game server.
				Command:      []string{"/bin/sh", "-c", "exec " + renderStartCommand(egg.Spec.StartCommand, vars)},
				WorkingDir:   dataMountPath,
				Env:          env,
				Ports:        containerPorts,
				Resources:    gs.Spec.Resources,
				VolumeMounts: mounts,
				// Stdin/TTY are enabled so the Panel API can attach to this
				// container's console (pods/attach) and forward player/admin
				// commands to the server process, mirroring how Wings' console
				// works against a locally-run process.
				Stdin:     true,
				StdinOnce: false,
				TTY:       false,
			},
				sftpagent.Container(sftpAgentImage, secretName, string(gs.UID), dataVolumeName, dataMountPath),
			},
			Volumes: volumes,
		},
	}
	return pod, nil
}

func installCommand(install *gameserversv1alpha1.EggInstall) []string {
	if len(install.Entrypoint) > 0 {
		return append(append([]string{}, install.Entrypoint...), install.Script)
	}
	return []string{"/bin/sh", "-c", install.Script}
}

// resolveVariables merges an Egg's declared variables (defaults) with a
// GameServer's overrides, and fails closed if a required variable ends up
// without a value. A validating webhook will move this check to admission
// time in a later milestone; the controller still enforces it so a GameServer
// created before the webhook existed cannot silently run with missing config.
func resolveVariables(egg *gameserversv1alpha1.Egg, gs *gameserversv1alpha1.GameServer) (map[string]string, error) {
	resolved := make(map[string]string, len(egg.Spec.Variables))
	for _, v := range egg.Spec.Variables {
		resolved[v.Name] = v.Default
	}
	for _, override := range gs.Spec.Variables {
		resolved[override.Name] = override.Value
	}
	for _, v := range egg.Spec.Variables {
		if v.Required && resolved[v.Name] == "" {
			return nil, fmt.Errorf("variable %q is required by egg %q but has no value", v.Name, egg.Name)
		}
	}
	return resolved, nil
}

func renderStartCommand(template string, vars map[string]string) string {
	rendered := template
	for name, value := range vars {
		rendered = strings.ReplaceAll(rendered, "{{"+name+"}}", value)
	}
	return rendered
}

// SetupWithManager sets up the controller with the Manager.
func (r *GameServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gameserversv1alpha1.GameServer{}).
		Owns(&corev1.Pod{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&corev1.Secret{}).
		Named("gameserver").
		Complete(r)
}
