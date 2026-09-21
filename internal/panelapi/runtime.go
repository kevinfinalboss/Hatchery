package panelapi

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/sftpagent"
)

// diskUsageTTL is how long a `du` result is reused. The UI polls every few
// seconds and walking a large world directory is not free.
const diskUsageTTL = 60 * time.Second

// diskUsageTimeout bounds one `du`; a volume too big to walk in time simply
// reports no disk figure instead of holding the request open.
const diskUsageTimeout = 8 * time.Second

type terminationJSON struct {
	// Container is "server" or "install" (the init container that runs the Egg's
	// install script), so a failed install is told apart from a crashed game.
	Container  string     `json:"container"`
	Reason     string     `json:"reason,omitempty"`
	ExitCode   int32      `json:"exitCode"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

type diskJSON struct {
	UsedBytes  int64 `json:"usedBytes"`
	TotalBytes int64 `json:"totalBytes,omitempty"`
}

type runtimeResponse struct {
	// StartedAt is when the game container started; only set while it runs.
	StartedAt *time.Time `json:"startedAt,omitempty"`
	// Terminated describes why the last run of the Pod ended, when it did.
	Terminated *terminationJSON `json:"terminated,omitempty"`
	// Disk is only reported while the game container is running.
	Disk *diskJSON `json:"disk,omitempty"`
}

// podRuntime reads uptime and the reason a run ended straight off the Pod
// status. The Pod is created with RestartPolicy Never, so a container never
// restarts in place: a crash leaves state.terminated on the same Pod, and
// restartCount would always be 0 (which is why it isn't reported).
func podRuntime(pod *corev1.Pod) (started *time.Time, term *terminationJSON) {
	for _, st := range pod.Status.InitContainerStatuses {
		if t := st.State.Terminated; t != nil && t.ExitCode != 0 {
			return nil, terminationOf("install", t)
		}
	}
	for _, st := range pod.Status.ContainerStatuses {
		if st.Name != gameContainerName {
			continue
		}
		switch {
		case st.State.Running != nil:
			t := st.State.Running.StartedAt.Time
			return &t, nil
		case st.State.Terminated != nil:
			return nil, terminationOf(gameContainerName, st.State.Terminated)
		}
	}
	return nil, nil
}

func terminationOf(container string, t *corev1.ContainerStateTerminated) *terminationJSON {
	out := &terminationJSON{Container: container, Reason: t.Reason, ExitCode: t.ExitCode}
	if !t.FinishedAt.IsZero() {
		f := t.FinishedAt.Time
		out.FinishedAt = &f
	}
	return out
}

// parseDuKilobytes reads the size out of `du -sk` output ("<KiB>\t<path>").
func parseDuKilobytes(out string) (int64, error) {
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return 0, fmt.Errorf("empty du output")
	}
	kb, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil || kb < 0 {
		return 0, fmt.Errorf("unexpected du output %q", out)
	}
	return kb * 1024, nil
}

type diskCacheEntry struct {
	bytes   int64
	expires time.Time
}

// diskCache memoizes du results per Pod (UID in the key, so a recreated Pod
// never inherits the previous one's number).
type diskCache struct {
	mu sync.Mutex
	m  map[string]diskCacheEntry
}

func (c *diskCache) get(key string, now time.Time) (int64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok || !now.Before(e.expires) {
		delete(c.m, key)
		return 0, false
	}
	return e.bytes, true
}

func (c *diskCache) put(key string, b int64, expires time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]diskCacheEntry{}
	}
	c.m[key] = diskCacheEntry{bytes: b, expires: expires}
}

// execInPod runs cmd in one container of a Pod and returns its stdout. Tests
// replace Server.execFn.
func (s *Server) execInPod(ctx context.Context, namespace, pod, container string, cmd []string) (string, error) {
	req := s.Clientset.CoreV1().RESTClient().Post().
		Resource("pods").Namespace(namespace).Name(pod).SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   cmd,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)
	executor, err := remotecommand.NewSPDYExecutor(s.RESTConfig, http.MethodPost, req.URL())
	if err != nil {
		return "", err
	}
	var stdout, stderr bytes.Buffer
	if err := executor.StreamWithContext(ctx, remotecommand.StreamOptions{Stdout: &stdout, Stderr: &stderr}); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// diskUsage returns how many bytes the game's data directory holds, or ok=false
// when it can't be measured (no `du` in the image, timeout, ...). Not being
// able to measure is a normal outcome, not an error worth failing the request.
func (s *Server) diskUsage(ctx context.Context, pod *corev1.Pod) (int64, bool) {
	key := string(pod.UID)
	if key == "" {
		key = pod.Namespace + "/" + pod.Name
	}
	if b, ok := s.disk.get(key, time.Now()); ok {
		return b, true
	}
	exec := s.execFn
	if exec == nil {
		exec = s.execInPod
	}
	ctx, cancel := context.WithTimeout(ctx, diskUsageTimeout)
	defer cancel()
	out, err := exec(ctx, pod.Namespace, pod.Name, gameContainerName, []string{"du", "-sk", sftpagent.DefaultDataMountPath})
	if err != nil {
		return 0, false
	}
	b, err := parseDuKilobytes(out)
	if err != nil {
		return 0, false
	}
	s.disk.put(key, b, time.Now().Add(diskUsageTTL))
	return b, true
}

// handleRuntime reports how the GameServer's Pod is doing right now: uptime,
// why the last run ended, and how full the data volume is. A server with no
// Pod (Stopped) answers with an empty object.
func (s *Server) handleRuntime(w http.ResponseWriter, r *http.Request) {
	if !s.requireGameServerAccess(w, r) {
		return
	}
	var gs gameserversv1alpha1.GameServer
	if err := s.Client.Get(r.Context(), gameServerKey(r), &gs); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}

	var pod corev1.Pod
	if err := s.Client.Get(r.Context(), client.ObjectKey{Namespace: gs.Namespace, Name: gs.Name}, &pod); err != nil {
		if statusFor(err) == http.StatusNotFound {
			writeJSON(w, http.StatusOK, runtimeResponse{})
			return
		}
		writeError(w, statusFor(err), err.Error())
		return
	}

	resp := runtimeResponse{}
	resp.StartedAt, resp.Terminated = podRuntime(&pod)
	if resp.StartedAt != nil {
		if used, ok := s.diskUsage(r.Context(), &pod); ok {
			resp.Disk = &diskJSON{UsedBytes: used}
			if q, err := resource.ParseQuantity(gs.Spec.Storage.Size); err == nil {
				resp.Disk.TotalBytes = q.Value()
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
