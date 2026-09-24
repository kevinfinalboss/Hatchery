package controller

import (
	"context"
	"regexp"
	"time"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

const (
	// serverContainerName is the game container; the sftp-agent sidecar shares the Pod.
	serverContainerName = "server"
	// startupLogLines is how much of the console's tail is matched against the Egg's startup regex.
	startupLogLines = 500
	// startupPollInterval is how often the console is re-read while a server is starting.
	startupPollInterval = 3 * time.Second
	// defaultStartupTimeout is how long to wait for the startup regex before giving up on it.
	defaultStartupTimeout = 600 * time.Second
)

// LogReader reads the tail of a container's console. The real one (ClientsetLogReader) goes through
// the API server's pods/log; tests use a fake.
type LogReader interface {
	TailLogs(ctx context.Context, namespace, pod, container string, lines int64) (string, error)
}

// ClientsetLogReader is the LogReader backed by client-go.
type ClientsetLogReader struct{ Clientset kubernetes.Interface }

func (c ClientsetLogReader) TailLogs(ctx context.Context, namespace, pod, container string, lines int64) (string, error) {
	raw, err := c.Clientset.CoreV1().Pods(namespace).GetLogs(pod, &corev1.PodLogOptions{
		Container: container,
		TailLines: &lines,
	}).DoRaw(ctx)
	return string(raw), err
}

// observation is what the controller concludes about a server from its Pod.
type observation struct {
	phase   gameserversv1alpha1.GameServerPhase
	podName string
	ready   metav1.Condition
	requeue time.Duration
}

func readyCondition(gs *gameserversv1alpha1.GameServer, status metav1.ConditionStatus, reason string) metav1.Condition {
	return metav1.Condition{
		Type:               gameserversv1alpha1.ConditionReady,
		Status:             status,
		Reason:             reason,
		ObservedGeneration: gs.Generation,
	}
}

// observe derives the phase and the Ready condition. pod is nil when there is none.
func (r *GameServerReconciler) observe(ctx context.Context, gs *gameserversv1alpha1.GameServer, egg *gameserversv1alpha1.Egg, pod *corev1.Pod) observation {
	notReady := func(reason string) metav1.Condition { return readyCondition(gs, metav1.ConditionFalse, reason) }

	stopped := gs.Spec.State == gameserversv1alpha1.GameServerStateStopped || gs.Spec.Suspended
	switch {
	case pod == nil && gs.Spec.Suspended:
		return observation{phase: gameserversv1alpha1.GameServerPhaseSuspended, ready: notReady("Suspended")}
	case pod == nil && stopped:
		return observation{phase: gameserversv1alpha1.GameServerPhaseStopped, ready: notReady("NotRunning")}
	case pod == nil:
		return observation{phase: gameserversv1alpha1.GameServerPhasePending, ready: notReady("NotRunning")}
	}

	// A Pod that is going away — being stopped, or replaced for a restart — is Stopping. The
	// controller has just deleted it in the same reconcile, so the local object may not show it yet.
	if !pod.DeletionTimestamp.IsZero() || stopped || restartRequested(gs, pod) {
		return observation{phase: gameserversv1alpha1.GameServerPhaseStopping, podName: pod.Name, ready: notReady("Stopping")}
	}

	// The game container ended on its own while the sftp-agent sidecar keeps the Pod Running: without
	// this the server would show Running (or Starting forever, with startup detection) with no game.
	if serverTerminated(pod) != nil {
		return observation{phase: gameserversv1alpha1.GameServerPhaseCrashed, podName: pod.Name, ready: notReady("Crashed")}
	}

	switch pod.Status.Phase {
	case corev1.PodFailed:
		return observation{phase: gameserversv1alpha1.GameServerPhaseFailed, podName: pod.Name, ready: notReady("Failed")}
	case corev1.PodRunning:
		return r.observeRunning(ctx, gs, egg, pod)
	}
	for _, c := range pod.Status.InitContainerStatuses {
		if c.State.Running != nil {
			return observation{phase: gameserversv1alpha1.GameServerPhaseInstalling, podName: pod.Name, ready: notReady("Installing")}
		}
	}
	return observation{phase: gameserversv1alpha1.GameServerPhasePending, podName: pod.Name, ready: notReady("Pending")}
}

func (r *GameServerReconciler) observeRunning(ctx context.Context, gs *gameserversv1alpha1.GameServer, egg *gameserversv1alpha1.Egg, pod *corev1.Pod) observation {
	log := logf.FromContext(ctx)
	running := func(status metav1.ConditionStatus, reason string) observation {
		return observation{phase: gameserversv1alpha1.GameServerPhaseRunning, podName: pod.Name, ready: readyCondition(gs, status, reason)}
	}
	starting := func(reason string) observation {
		return observation{
			phase: gameserversv1alpha1.GameServerPhaseStarting, podName: pod.Name,
			ready: readyCondition(gs, metav1.ConditionFalse, reason), requeue: startupPollInterval,
		}
	}

	det := egg.Spec.StartupDetection
	if det == nil || r.LogReader == nil {
		return running(metav1.ConditionTrue, "NoStartupDetection")
	}
	re, err := regexp.Compile(det.Regex)
	if err != nil {
		log.Error(err, "the Egg's startupDetection.regex does not compile; ignoring it", "egg", egg.Name)
		return running(metav1.ConditionTrue, "NoStartupDetection")
	}

	var startedAt time.Time
	for _, c := range pod.Status.ContainerStatuses {
		if c.Name == serverContainerName && c.State.Running != nil {
			startedAt = c.State.Running.StartedAt.Time
		}
	}
	if startedAt.IsZero() {
		return starting("Starting") // the game container has not started yet
	}

	// Ready carried over from before is only valid for the container run it was recorded in.
	if cond := apimeta.FindStatusCondition(gs.Status.Conditions, gameserversv1alpha1.ConditionReady); cond != nil &&
		cond.Status == metav1.ConditionTrue && cond.Reason == "StartupDetected" && !cond.LastTransitionTime.Time.Before(startedAt) {
		return running(metav1.ConditionTrue, "StartupDetected")
	}

	timeout := defaultStartupTimeout
	if det.TimeoutSeconds != nil {
		timeout = time.Duration(*det.TimeoutSeconds) * time.Second
	}
	if time.Since(startedAt) > timeout {
		return running(metav1.ConditionFalse, "StartupTimeout")
	}

	out, err := r.LogReader.TailLogs(ctx, pod.Namespace, pod.Name, serverContainerName, startupLogLines)
	if err != nil {
		log.V(1).Info("could not read the console to detect startup", "error", err.Error())
		return starting("Starting")
	}
	if re.MatchString(out) {
		return running(metav1.ConditionTrue, "StartupDetected")
	}
	return starting("Starting")
}
