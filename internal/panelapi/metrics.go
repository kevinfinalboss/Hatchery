package panelapi

import (
	"net/http"
	"time"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/panelcache"
)

// metricsWindow describes one range the UI can ask for and the bucket its
// points are averaged into (keeps the response near 100 points however wide
// the window is).
type metricsWindow struct {
	span   time.Duration
	bucket time.Duration
}

var metricsWindows = map[string]metricsWindow{
	"15m": {15 * time.Minute, 15 * time.Second},
	"1h":  {time.Hour, 30 * time.Second},
	"6h":  {6 * time.Hour, 3 * time.Minute},
}

type metricPointJSON struct {
	T             int64 `json:"t"`
	CPUMillicores int64 `json:"cpuMillicores"`
	MemoryBytes   int64 `json:"memoryBytes"`
}

type metricsResponse struct {
	IntervalSeconds int               `json:"intervalSeconds"`
	Points          []metricPointJSON `json:"points"`
}

// downsample averages samples (oldest first) into fixed buckets. Each output
// point carries the timestamp of the newest sample in its bucket.
func downsample(samples []panelcache.MetricSample, bucket time.Duration) []metricPointJSON {
	points := []metricPointJSON{}
	b := bucket.Milliseconds()
	var (
		cur            int64 = -1
		n, cpu, mem, t int64
	)
	flush := func() {
		if n > 0 {
			points = append(points, metricPointJSON{T: t, CPUMillicores: cpu / n, MemoryBytes: mem / n})
		}
		n, cpu, mem = 0, 0, 0
	}
	for _, s := range samples {
		if idx := s.T / b; idx != cur {
			flush()
			cur = idx
		}
		n++
		cpu += s.CPUMillicores
		mem += s.MemoryBytes
		t = s.T
	}
	flush()
	return points
}

// handleMetrics serves the CPU/memory history the sampler (metricssampler.go)
// has collected for one GameServer. A GameServer that isn't running, or hasn't
// been sampled yet, answers with no points rather than an error.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if !s.requireGameServerAccess(w, r) {
		return
	}
	if s.Metrics == nil {
		writeError(w, http.StatusNotImplemented, "metrics are not enabled")
		return
	}
	rng := r.URL.Query().Get("range")
	if rng == "" {
		rng = "15m"
	}
	win, ok := metricsWindows[rng]
	if !ok {
		writeError(w, http.StatusBadRequest, "range must be one of 15m, 1h, 6h")
		return
	}

	var gs gameserversv1alpha1.GameServer
	if err := s.Client.Get(r.Context(), gameServerKey(r), &gs); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}

	since := time.Now().Add(-win.span).UnixMilli()
	samples, err := s.Metrics.Range(r.Context(), gs.Namespace, gs.Name, since)
	if err != nil {
		writeError(w, http.StatusBadGateway, "reading metrics: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, metricsResponse{
		IntervalSeconds: int(win.bucket.Seconds()),
		Points:          downsample(samples, win.bucket),
	})
}
