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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
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

	// LogReader reads a game container's console so the startup regex of an Egg can be matched.
	// nil disables startup detection: servers go straight from Pending to Running.
	LogReader LogReader
}

// +kubebuilder:rbac:groups=gameservers.hatchery.io,resources=gameservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gameservers.hatchery.io,resources=gameservers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gameservers.hatchery.io,resources=gameservers/finalizers,verbs=update
// +kubebuilder:rbac:groups=gameservers.hatchery.io,resources=eggs,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods/log,verbs=get
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
	eggKey := types.NamespacedName{Namespace: gs.EggNamespace(), Name: gs.Spec.EggRef.Name}
	if err := r.Get(ctx, eggKey, &egg); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("referenced Egg not found, waiting", "egg", eggKey.Name)
			return r.setPhase(ctx, &gs, gameserversv1alpha1.GameServerPhaseFailed)
		}
		return ctrl.Result{}, err
	}

	if _, ok := egg.ResolveImage(gs.Spec.ImageName); !ok {
		log.Info("the Egg does not declare the requested image", "egg", eggKey.Name, "imageName", gs.Spec.ImageName)
		return r.setPhase(ctx, &gs, gameserversv1alpha1.GameServerPhaseFailed)
	}

	if err := r.reconcilePVC(ctx, &gs); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling pvc: %w", err)
	}

	if err := r.reconcileService(ctx, &gs, &egg); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling service: %w", err)
	}

	if err := r.reconcileNetworkPolicy(ctx, &gs, &egg); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling network policy: %w", err)
	}

	pod, err := r.reconcilePod(ctx, &gs, &egg)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling pod: %w", err)
	}

	return r.updateStatus(ctx, &gs, &egg, pod)
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

// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch

// gameIngressPolicySpec allows player traffic to one GameServer's pods on
// exactly the ports its Egg declares, from any source. It is additive to the
// tenant namespace's default-deny (see tenantNetworkPolicies). An Egg with no
// ports gets no rule at all: an ingress rule with an empty port list would
// mean "every port".
func gameIngressPolicySpec(gsName string, egg *gameserversv1alpha1.Egg) networkingv1.NetworkPolicySpec {
	spec := networkingv1.NetworkPolicySpec{
		PodSelector: metav1.LabelSelector{MatchLabels: gameServerLabels(gsName)},
		PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
	}
	var ports []networkingv1.NetworkPolicyPort
	for _, p := range egg.Spec.Ports {
		proto := p.Protocol
		if proto == "" {
			proto = corev1.ProtocolTCP
		}
		port := intstr.FromInt32(p.ContainerPort)
		ports = append(ports, networkingv1.NetworkPolicyPort{Protocol: &proto, Port: &port})
	}
	if len(ports) > 0 {
		spec.Ingress = []networkingv1.NetworkPolicyIngressRule{{Ports: ports}}
	}
	return spec
}

// reconcileNetworkPolicy opens the Egg's ports to players, but only in tenant
// namespaces. In any other namespace nothing is default-denied, and adding an
// ingress policy there would *isolate* the pods for ingress and cut off the
// sftp-agent port — the opposite of what this is for.
func (r *GameServerReconciler) reconcileNetworkPolicy(ctx context.Context, gs *gameserversv1alpha1.GameServer, egg *gameserversv1alpha1.Egg) error {
	var ns corev1.Namespace
	if err := r.Get(ctx, types.NamespacedName{Name: gs.Namespace}, &ns); err != nil {
		return err
	}
	if ns.Labels[gameserversv1alpha1.LabelTenant] == "" {
		return nil
	}
	np := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: gs.Name + "-game", Namespace: gs.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, np, func() error {
		np.Spec = gameIngressPolicySpec(gs.Name, egg)
		return controllerutil.SetControllerReference(gs, np, r.Scheme)
	})
	return err
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

func (r *GameServerReconciler) reconcilePod(ctx context.Context, gs *gameserversv1alpha1.GameServer, egg *gameserversv1alpha1.Egg) (*corev1.Pod, error) {
	var pod corev1.Pod
	err := r.Get(ctx, types.NamespacedName{Namespace: gs.Namespace, Name: gs.Name}, &pod)
	exists := err == nil
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, err
	}

	// A Pod that is being deleted is still returned, so the status can say Stopping while its
	// preStop hook gives the game time to shut down.
	if gs.Spec.State == gameserversv1alpha1.GameServerStateStopped || gs.Spec.Suspended {
		if exists {
			if pod.DeletionTimestamp.IsZero() {
				if err := r.Delete(ctx, &pod); err != nil {
					return nil, client.IgnoreNotFound(err)
				}
			}
			return &pod, nil
		}
		return nil, nil
	}

	if exists {
		if pod.DeletionTimestamp.IsZero() && restartRequested(gs, &pod) {
			if err := r.Delete(ctx, &pod); err != nil {
				return nil, client.IgnoreNotFound(err)
			}
		}
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

// restartRequested reports whether the GameServer asks for a restart the running Pod
// hasn't been through yet: it carries a RestartAnnotation the Pod's stamp doesn't match.
// A Pod created before restarts existed has no stamp, so the first request restarts it too.
func restartRequested(gs *gameserversv1alpha1.GameServer, pod *corev1.Pod) bool {
	want := gs.Annotations[gameserversv1alpha1.RestartAnnotation]
	return want != "" && want != pod.Annotations[gameserversv1alpha1.RestartAnnotation]
}

// specHash fingerprints the parts of a GameServer's spec that only take effect when its Pod is
// (re)created. displayName is deliberately excluded: renaming never needs a restart.
func specHash(gs *gameserversv1alpha1.GameServer) string {
	vars := append([]gameserversv1alpha1.GameServerVariable(nil), gs.Spec.Variables...)
	sort.Slice(vars, func(i, j int) bool { return vars[i].Name < vars[j].Name })
	raw, _ := json.Marshal(struct {
		Variables []gameserversv1alpha1.GameServerVariable `json:"v"`
		Resources corev1.ResourceRequirements              `json:"r"`
		ImageName string                                   `json:"i"`
		StartCmd  string                                   `json:"c"`
	}{vars, gs.Spec.Resources, gs.Spec.ImageName, gs.Spec.StartCommand})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:8])
}

// restartRequiredCondition reports whether the running Pod was built from an older spec than the
// GameServer's current one. A Pod without a hash stamp (created before this existed) is not flagged.
func restartRequiredCondition(gs *gameserversv1alpha1.GameServer, pod *corev1.Pod) metav1.Condition {
	cond := metav1.Condition{
		Type:               gameserversv1alpha1.ConditionRestartRequired,
		Status:             metav1.ConditionFalse,
		Reason:             "UpToDate",
		ObservedGeneration: gs.Generation,
	}
	if pod != nil {
		if h := pod.Annotations[gameserversv1alpha1.SpecHashAnnotation]; h != "" && h != specHash(gs) {
			cond.Status = metav1.ConditionTrue
			cond.Reason = "SpecChanged"
			cond.Message = "the running server was started before the last variable or resource change"
		}
	}
	return cond
}

// updateStatus recomputes GameServerStatus from the live Pod (if any) and
// patches it when it drifted from what's stored.
func (r *GameServerReconciler) updateStatus(ctx context.Context, gs *gameserversv1alpha1.GameServer, egg *gameserversv1alpha1.Egg, pod *corev1.Pod) (ctrl.Result, error) {
	obs := r.observe(ctx, gs, egg, pod)
	result := ctrl.Result{RequeueAfter: obs.requeue}

	condChanged := apimeta.SetStatusCondition(&gs.Status.Conditions, obs.ready)
	condChanged = apimeta.SetStatusCondition(&gs.Status.Conditions, restartRequiredCondition(gs, pod)) || condChanged
	if !condChanged && gs.Status.Phase == obs.phase && gs.Status.PodName == obs.podName && gs.Status.ObservedGeneration == gs.Generation {
		return result, nil
	}

	oldPhase := gs.Status.Phase
	gs.Status.Phase = obs.phase
	gs.Status.PodName = obs.podName
	gs.Status.ObservedGeneration = gs.Generation
	if err := r.Status().Update(ctx, gs); err != nil {
		return ctrl.Result{}, err
	}
	recordGameServerPhase(gs, oldPhase, obs.phase)
	return result, nil
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

	serverImage, ok := egg.ResolveImage(gs.Spec.ImageName)
	if !ok {
		return nil, fmt.Errorf("egg %q does not declare image %q", egg.Name, gs.Spec.ImageName)
	}

	var initContainers []corev1.Container
	if egg.Spec.Install != nil {
		image := egg.Spec.Install.Image
		if image == "" {
			image = serverImage
		}
		initContainers = append(initContainers, corev1.Container{
			Name:            "install",
			Image:           image,
			WorkingDir:      dataMountPath,
			Command:         onceInstallCommand(egg.Spec.Install),
			Env:             append(append([]corev1.EnvVar{}, env...), corev1.EnvVar{Name: installRevisionEnv, Value: strconv.FormatInt(gs.Spec.InstallRevision, 10)}),
			VolumeMounts:    mounts,
			SecurityContext: gameContainerSecurityContext(),
		})
	}
	if c := egg.Spec.Configure; c != nil {
		image := c.Image
		if image == "" {
			image = serverImage
		}
		entrypoint := c.Entrypoint
		if len(entrypoint) == 0 {
			entrypoint = []string{"/bin/sh", "-c"}
		}
		initContainers = append(initContainers, corev1.Container{
			Name:            "configure",
			Image:           image,
			WorkingDir:      dataMountPath,
			Command:         append(append([]string{}, entrypoint...), c.Script),
			Env:             env,
			VolumeMounts:    mounts,
			SecurityContext: gameContainerSecurityContext(),
		})
	}
	automountToken := false

	serverEnv := env
	if egg.Spec.StopCommand != "" {
		serverEnv = append(append([]corev1.EnvVar{}, env...), corev1.EnvVar{Name: stopCommandEnv, Value: egg.Spec.StopCommand})
	}
	graceSeconds := int64(defaultStopTimeoutSeconds)
	if t := egg.Spec.StopTimeoutSeconds; t != nil {
		graceSeconds = int64(*t)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        gs.Name,
			Namespace:   gs.Namespace,
			Labels:      gameServerLabels(gs.Name),
			Annotations: podAnnotations(gs),
		},
		Spec: corev1.PodSpec{
			RestartPolicy:                 corev1.RestartPolicyNever,
			AutomountServiceAccountToken:  &automountToken,
			TerminationGracePeriodSeconds: &graceSeconds,
			InitContainers:                initContainers,
			// fsGroup lets the sftp-agent sidecar and the "server" container
			// share files on the data volume regardless of which uid the
			// Egg's image runs the server as — see sftpagent.SharedFSGroup.
			SecurityContext: sftpagent.PodSecurityContext(),
			Containers: []corev1.Container{{
				Name:  "server",
				Image: serverImage,
				// "exec" makes a POSIX shell replace itself with the target
				// process instead of forking it, so the server binary — not
				// /bin/sh — ends up as the container's PID 1. That matters for
				// two things the Panel API depends on: `kubectl attach`-style
				// console access writes to PID 1's stdin, and StopSignal is
				// delivered to PID 1. Without "exec" both would silently hit
				// the shell instead of the game server.
				Command:      []string{"/bin/sh", "-c", "exec " + renderStartCommand(egg.EffectiveStartCommand(gs.Spec.StartCommand), vars)},
				WorkingDir:   dataMountPath,
				Env:          serverEnv,
				Lifecycle:    stopLifecycle(egg),
				Ports:        containerPorts,
				Resources:    gs.Spec.Resources,
				VolumeMounts: mounts,
				// Stdin/TTY are enabled so the Panel API can attach to this
				// container's console (pods/attach) and forward player/admin
				// commands to the server process, mirroring how Wings' console
				// works against a locally-run process.
				Stdin:           true,
				StdinOnce:       false,
				TTY:             false,
				SecurityContext: gameContainerSecurityContext(),
			},
				sftpagent.Container(sftpAgentImage, secretName, string(gs.UID), dataVolumeName, dataMountPath),
			},
			Volumes: volumes,
		},
	}
	return pod, nil
}

func gameContainerSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{AllowPrivilegeEscalation: ptr.To(false)}
}

// podAnnotations stamps the Pod with the GameServer's current RestartAnnotation, so a
// restart that was already served isn't served again by the Pod that replaced the old one.
func podAnnotations(gs *gameserversv1alpha1.GameServer) map[string]string {
	ann := map[string]string{gameserversv1alpha1.SpecHashAnnotation: specHash(gs)}
	if v := gs.Annotations[gameserversv1alpha1.RestartAnnotation]; v != "" {
		ann[gameserversv1alpha1.RestartAnnotation] = v
	}
	return ann
}

const (
	// installRevisionEnv carries spec.installRevision into the install container.
	installRevisionEnv = "HATCHERY_INSTALL_REVISION"
	// stopCommandEnv carries the Egg's stopCommand into the game container, so the preStop hook
	// never has to interpolate it into a shell string.
	stopCommandEnv = "HATCHERY_STOP_COMMAND"
	// defaultStopTimeoutSeconds is how long a server may take to stop when the Egg does not say.
	defaultStopTimeoutSeconds = 60
	// installMarker on the data volume records which installRevision was last installed.
	installMarker = dataMountPath + "/.hatchery-installed"
)

// onceInstallCommand wraps the Egg's install command so it only runs when the marker on the data
// volume differs from the server's installRevision. The marker is written only when the script
// succeeds, so a failed install is retried on the next start. The Egg's own entrypoint and script
// are passed through untouched, as "$@".
func onceInstallCommand(install *gameserversv1alpha1.EggInstall) []string {
	wrapper := fmt.Sprintf(`if [ "$(cat %[1]s 2>/dev/null)" = "$%[2]s" ]; then
  echo "install already done (revision $%[2]s), skipping"
  exit 0
fi
"$@" || exit $?
printf '%%s' "$%[2]s" > %[1]s
`, installMarker, installRevisionEnv)
	return append([]string{"/bin/sh", "-c", wrapper, "hatchery-install"}, installCommand(install)...)
}

var signalName = regexp.MustCompile(`^[A-Z0-9]{1,10}$`)

// stopLifecycle builds the preStop hook that stops the game gracefully before the kubelet's own
// SIGTERM: it writes the Egg's stopCommand to the game's stdin (PID 1), or, without a command,
// sends a non-default stopSignal, then waits for the process to exit. lifecycle.stopSignal is not
// used because it needs a feature gate that is off on many clusters. nil means "nothing to do".
func stopLifecycle(egg *gameserversv1alpha1.Egg) *corev1.Lifecycle {
	const wait = `while kill -0 1 2>/dev/null; do sleep 1; done`
	var script string
	switch sig := strings.TrimPrefix(strings.ToUpper(egg.Spec.StopSignal), "SIG"); {
	case egg.Spec.StopCommand != "":
		script = `printf '%s\n' "$` + stopCommandEnv + `" > /proc/1/fd/0; ` + wait
	case sig != "" && sig != "TERM" && signalName.MatchString(sig):
		script = "kill -" + sig + " 1; " + wait
	default:
		return nil
	}
	return &corev1.Lifecycle{PreStop: &corev1.LifecycleHandler{
		Exec: &corev1.ExecAction{Command: []string{"/bin/sh", "-c", script}},
	}}
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
		Owns(&networkingv1.NetworkPolicy{}).
		Named("gameserver").
		Complete(r)
}
