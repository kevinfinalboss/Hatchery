package panelapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/panelcache"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDownsampleAveragesPerBucket(t *testing.T) {
	got := downsample([]panelcache.MetricSample{
		{T: 0, CPUMillicores: 100, MemoryBytes: 1000},
		{T: 10_000, CPUMillicores: 300, MemoryBytes: 3000},
		{T: 30_000, CPUMillicores: 50, MemoryBytes: 500},
		{T: 40_000, CPUMillicores: 70, MemoryBytes: 700},
	}, 30*time.Second)
	want := []metricPointJSON{
		{T: 10_000, CPUMillicores: 200, MemoryBytes: 2000},
		{T: 40_000, CPUMillicores: 60, MemoryBytes: 600},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("downsample = %+v, want %+v", got, want)
	}
	if out := downsample(nil, time.Minute); out == nil || len(out) != 0 {
		t.Fatalf("empty input must give an empty, non-nil slice (JSON [] not null), got %#v", out)
	}
}

const podMetricsJSON = `{"items":[
 {"metadata":{"name":"mc"},"timestamp":"2026-09-20T12:00:00Z","containers":[
   {"name":"server","usage":{"cpu":"250m","memory":"1Gi"}},
   {"name":"sftp-agent","usage":{"cpu":"1m","memory":"8Mi"}}]},
 {"metadata":{"name":"tr"},"timestamp":"2026-09-20T12:00:00Z","containers":[
   {"name":"server","usage":{"cpu":"12345678n","memory":"512Mi"}}]}]}`

func TestParsePodMetricsOnlyReadsGameContainer(t *testing.T) {
	got, err := parsePodMetrics([]byte(podMetricsJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d metrics, want 2 (sidecar must be ignored): %+v", len(got), got)
	}
	if got[0].Pod != "mc" || got[0].CPUMillicores != 250 || got[0].MemoryBytes != 1<<30 {
		t.Fatalf("mc = %+v", got[0])
	}
	if got[1].CPUMillicores != 13 { // 12.3m rounds up
		t.Fatalf("nanocores not converted: %+v", got[1])
	}
}

func activeTenant(name string) *gameserversv1alpha1.Tenant {
	return &gameserversv1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: gameserversv1alpha1.TenantStatus{
			Phase:     gameserversv1alpha1.TenantPhaseActive,
			Namespace: gameserversv1alpha1.TenantNamespace(name),
		},
	}
}

func TestSamplerStoresSamplesPerTenantNamespace(t *testing.T) {
	srv := newTestServer(t, activeTenant("acme"))
	store := panelcache.NewMemoryMetricsStore()
	ts := time.Now().Truncate(time.Second)
	s := &MetricsSampler{
		Client: srv.Client,
		Store:  store,
		Source: func(_ context.Context, ns string) ([]PodMetric, error) {
			if ns != gameserversv1alpha1.TenantNamespace("acme") {
				t.Errorf("sampled unexpected namespace %q", ns)
			}
			return []PodMetric{{Pod: "mc", Timestamp: ts, CPUMillicores: 400, MemoryBytes: 2 << 30}}, nil
		},
	}
	s.SampleOnce(context.Background())
	s.SampleOnce(context.Background()) // same metric timestamp: must not duplicate

	got, _ := store.Range(context.Background(), gameserversv1alpha1.TenantNamespace("acme"), "mc", 0)
	if len(got) != 1 || got[0].CPUMillicores != 400 || got[0].T != ts.UnixMilli() {
		t.Fatalf("stored = %+v", got)
	}
}

func TestSamplerSurvivesSourceErrors(t *testing.T) {
	srv := newTestServer(t, activeTenant("acme"))
	s := &MetricsSampler{
		Client: srv.Client,
		Store:  panelcache.NewMemoryMetricsStore(),
		Source: func(context.Context, string) ([]PodMetric, error) { return nil, errors.New("no metrics-server") },
	}
	s.SampleOnce(context.Background()) // must not panic
	if s.lastErr == "" {
		t.Fatal("expected the failure to be remembered for log de-duplication")
	}
}

func createMetricsGameServer(t *testing.T, srv *Server) {
	t.Helper()
	gs := &gameserversv1alpha1.GameServer{
		ObjectMeta: metav1.ObjectMeta{Name: "mc", Namespace: testOrgNS()},
		Spec: gameserversv1alpha1.GameServerSpec{
			EggRef: gameserversv1alpha1.GameServerEggRef{Name: "minecraft"},
			State:  gameserversv1alpha1.GameServerStateRunning,
		},
	}
	if err := srv.Client.Create(context.Background(), gs); err != nil {
		t.Fatal(err)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	srv := newTestServer(t)
	store := panelcache.NewMemoryMetricsStore()
	srv.Metrics = store
	token := adminToken(t, srv)
	createMetricsGameServer(t, srv)

	now := time.Now()
	for i, age := range []time.Duration{2 * time.Hour, 10 * time.Minute, 5 * time.Minute} {
		_ = store.Append(context.Background(), testOrgNS(), "mc", panelcache.MetricSample{
			T: now.Add(-age).UnixMilli(), CPUMillicores: int64(100 * (i + 1)), MemoryBytes: 1 << 30,
		})
	}

	get := func(path string) (int, metricsResponse) {
		rec := doRequest(t, srv, http.MethodGet, orgURL(path), token, nil)
		var out metricsResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}

	code, out := get("/gameservers/mc/metrics") // default 15m
	if code != http.StatusOK || len(out.Points) != 2 || out.IntervalSeconds != 15 {
		t.Fatalf("15m: code=%d out=%+v", code, out)
	}
	if code, out = get("/gameservers/mc/metrics?range=6h"); code != http.StatusOK || len(out.Points) != 3 || out.IntervalSeconds != 180 {
		t.Fatalf("6h: code=%d out=%+v", code, out)
	}
	if code, _ = get("/gameservers/mc/metrics?range=1y"); code != http.StatusBadRequest {
		t.Fatalf("bad range: code=%d, want 400", code)
	}
	if code, _ = get("/gameservers/ghost/metrics"); code != http.StatusNotFound {
		t.Fatalf("unknown server: code=%d, want 404", code)
	}
}

func TestMetricsEndpointEmptyIsJSONArray(t *testing.T) {
	srv := newTestServer(t)
	srv.Metrics = panelcache.NewMemoryMetricsStore()
	token := adminToken(t, srv)
	createMetricsGameServer(t, srv)

	rec := doRequest(t, srv, http.MethodGet, orgURL("/gameservers/mc/metrics"), token, nil)
	if rec.Code != http.StatusOK || !json.Valid(rec.Body.Bytes()) {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	if string(raw["points"]) != "[]" {
		t.Fatalf("points = %s, want [] (the UI calls .map on it)", raw["points"])
	}
}

func TestMetricsEndpointDisabledIs501AndNeedsMembership(t *testing.T) {
	srv := newTestServer(t)
	token := adminToken(t, srv)
	createMetricsGameServer(t, srv)
	if rec := doRequest(t, srv, http.MethodGet, orgURL("/gameservers/mc/metrics"), token, nil); rec.Code != http.StatusNotImplemented {
		t.Fatalf("no store: code=%d, want 501", rec.Code)
	}

	srv.Metrics = panelcache.NewMemoryMetricsStore()
	outsider := newUserToken(t, srv, "outsider", false)
	if rec := doRequest(t, srv, http.MethodGet, orgURL("/gameservers/mc/metrics"), outsider, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("non-member: code=%d, want 404", rec.Code)
	}
	member := newMemberToken(t, srv, "viewer", paneldb.RoleMember)
	if rec := doRequest(t, srv, http.MethodGet, orgURL("/gameservers/mc/metrics"), member, nil); rec.Code != http.StatusOK {
		t.Fatalf("member: code=%d, want 200", rec.Code)
	}
}
