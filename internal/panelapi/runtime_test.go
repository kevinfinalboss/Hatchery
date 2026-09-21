package panelapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

func TestParseDuKilobytes(t *testing.T) {
	if b, err := parseDuKilobytes("2048\t/data\n"); err != nil || b != 2048*1024 {
		t.Fatalf("got %d, %v", b, err)
	}
	for _, bad := range []string{"", "abc /data", "-5 /data"} {
		if _, err := parseDuKilobytes(bad); err == nil {
			t.Fatalf("%q must be rejected", bad)
		}
	}
}

func TestPodRuntime(t *testing.T) {
	started := metav1.NewTime(time.Now().Add(-time.Hour))
	running := &corev1.Pod{Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
		{Name: "sftp-agent", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 9}}},
		{Name: "server", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: started}}},
	}}}
	if st, term := podRuntime(running); st == nil || !st.Equal(started.Time) || term != nil {
		t.Fatalf("running: started=%v term=%+v", st, term)
	}

	oom := &corev1.Pod{Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
		{Name: "server", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137, FinishedAt: started}}},
	}}}
	if st, term := podRuntime(oom); st != nil || term == nil || term.Reason != "OOMKilled" || term.ExitCode != 137 || term.Container != "server" {
		t.Fatalf("oom: started=%v term=%+v", st, term)
	}

	installFailed := &corev1.Pod{Status: corev1.PodStatus{
		InitContainerStatuses: []corev1.ContainerStatus{{Name: "install", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "Error", ExitCode: 35}}}},
	}}
	if _, term := podRuntime(installFailed); term == nil || term.Container != "install" || term.ExitCode != 35 {
		t.Fatalf("install failure must be reported: %+v", term)
	}

	okInstall := &corev1.Pod{Status: corev1.PodStatus{
		InitContainerStatuses: []corev1.ContainerStatus{{Name: "install", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "Completed", ExitCode: 0}}}},
	}}
	if st, term := podRuntime(okInstall); st != nil || term != nil {
		t.Fatalf("a finished install is not a failure: %v %+v", st, term)
	}
}

func runtimeFixture(t *testing.T, running bool) (*Server, string) {
	t.Helper()
	srv := newTestServer(t)
	token := adminToken(t, srv)
	ctx := context.Background()
	gs := &gameserversv1alpha1.GameServer{
		ObjectMeta: metav1.ObjectMeta{Name: "mc", Namespace: testOrgNS()},
		Spec: gameserversv1alpha1.GameServerSpec{
			EggRef:  gameserversv1alpha1.GameServerEggRef{Name: "minecraft"},
			State:   gameserversv1alpha1.GameServerStateRunning,
			Storage: gameserversv1alpha1.GameServerStorage{Size: "1Gi"},
		},
	}
	if err := srv.Client.Create(ctx, gs); err != nil {
		t.Fatal(err)
	}
	if running {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "mc", Namespace: testOrgNS(), UID: types.UID("uid-1")},
			Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
				{Name: "server", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.Now()}}},
			}},
		}
		if err := srv.Client.Create(ctx, pod); err != nil {
			t.Fatal(err)
		}
	}
	return srv, token
}

func getRuntime(t *testing.T, srv *Server, token string) (int, runtimeResponse) {
	t.Helper()
	rec := doRequest(t, srv, http.MethodGet, orgURL("/gameservers/mc/runtime"), token, nil)
	var out runtimeResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestRuntimeRunningReportsUptimeAndCachedDisk(t *testing.T) {
	srv, token := runtimeFixture(t, true)
	calls := 0
	srv.execFn = func(_ context.Context, ns, pod, container string, cmd []string) (string, error) {
		calls++
		if container != "server" || cmd[0] != "du" {
			t.Errorf("unexpected exec: %s %s %v", pod, container, cmd)
		}
		return "524288\t/data\n", nil // 512 MiB
	}
	for i := 0; i < 2; i++ {
		code, out := getRuntime(t, srv, token)
		if code != http.StatusOK || out.StartedAt == nil || out.Terminated != nil {
			t.Fatalf("code=%d out=%+v", code, out)
		}
		if out.Disk == nil || out.Disk.UsedBytes != 512<<20 || out.Disk.TotalBytes != 1<<30 {
			t.Fatalf("disk = %+v", out.Disk)
		}
	}
	if calls != 1 {
		t.Fatalf("du ran %d times; the second request must hit the cache", calls)
	}
}

func TestRuntimeDiskFailureIsNotAnError(t *testing.T) {
	srv, token := runtimeFixture(t, true)
	srv.execFn = func(context.Context, string, string, string, []string) (string, error) {
		return "", errors.New("du: not found")
	}
	code, out := getRuntime(t, srv, token)
	if code != http.StatusOK || out.StartedAt == nil || out.Disk != nil {
		t.Fatalf("code=%d out=%+v", code, out)
	}
}

func TestRuntimeWithoutPodIsEmpty(t *testing.T) {
	srv, token := runtimeFixture(t, false)
	srv.execFn = func(context.Context, string, string, string, []string) (string, error) {
		t.Error("must not exec without a Pod")
		return "", nil
	}
	code, out := getRuntime(t, srv, token)
	if code != http.StatusOK || out.StartedAt != nil || out.Terminated != nil || out.Disk != nil {
		t.Fatalf("code=%d out=%+v", code, out)
	}
	if rec := doRequest(t, srv, http.MethodGet, orgURL("/gameservers/ghost/runtime"), token, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown server: %d", rec.Code)
	}
}
