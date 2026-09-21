package panelapi

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/panelcache"
)

// DefaultMetricsInterval is how often the sampler polls metrics-server.
// metrics-server itself only refreshes about every 15s, so going faster only
// re-reads the same value.
const DefaultMetricsInterval = 15 * time.Second

// PodMetric is the slice of a metrics.k8s.io PodMetrics object the sampler
// needs: the game container's usage at Timestamp.
type PodMetric struct {
	Pod           string
	Timestamp     time.Time
	CPUMillicores int64
	MemoryBytes   int64
}

// PodMetricsSource lists current PodMetrics of the game containers in one
// namespace. Swapped for a fake in tests.
type PodMetricsSource func(ctx context.Context, namespace string) ([]PodMetric, error)

// MetricsSampler turns metrics-server's instantaneous readings into history:
// every Interval it reads the game container's usage of each GameServer pod in
// every Tenant namespace and appends it to Store.
type MetricsSampler struct {
	Client   client.Client
	Source   PodMetricsSource
	Store    panelcache.MetricsStore
	Interval time.Duration

	lastErr string
}

// NewMetricsSampler samples through the metrics.k8s.io API of clientset.
func NewMetricsSampler(c client.Client, clientset kubernetes.Interface, store panelcache.MetricsStore) *MetricsSampler {
	return &MetricsSampler{Client: c, Source: kubePodMetrics(clientset), Store: store, Interval: DefaultMetricsInterval}
}

// Run blocks until ctx is done.
func (m *MetricsSampler) Run(ctx context.Context) {
	interval := m.Interval
	if interval <= 0 {
		interval = DefaultMetricsInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		m.SampleOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// SampleOnce takes one round of samples. A failure in one namespace doesn't
// stop the others; errors are logged only when they change, so a cluster
// without metrics-server doesn't produce a line every 15 seconds.
func (m *MetricsSampler) SampleOnce(ctx context.Context) {
	logger := log.FromContext(ctx).WithName("metrics-sampler")

	var tenants gameserversv1alpha1.TenantList
	if err := m.Client.List(ctx, &tenants); err != nil {
		m.report(logger, fmt.Errorf("listing tenants: %w", err))
		return
	}

	var firstErr error
	for _, tn := range tenants.Items {
		if tn.Status.Phase != gameserversv1alpha1.TenantPhaseActive || tn.Status.Namespace == "" {
			continue
		}
		ns := tn.Status.Namespace
		pods, err := m.Source(ctx, ns)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("reading pod metrics in %s: %w", ns, err)
			}
			continue
		}
		for _, p := range pods {
			err := m.Store.Append(ctx, ns, p.Pod, panelcache.MetricSample{
				T:             p.Timestamp.UnixMilli(),
				CPUMillicores: p.CPUMillicores,
				MemoryBytes:   p.MemoryBytes,
			})
			if err != nil && firstErr == nil {
				firstErr = fmt.Errorf("storing metrics for %s/%s: %w", ns, p.Pod, err)
			}
		}
	}
	m.report(logger, firstErr)
}

func (m *MetricsSampler) report(logger interface {
	Error(error, string, ...any)
	Info(string, ...any)
}, err error) {
	switch {
	case err == nil && m.lastErr != "":
		logger.Info("metrics sampling recovered")
		m.lastErr = ""
	case err != nil && err.Error() != m.lastErr:
		logger.Error(err, "metrics sampling failed (is metrics-server installed?)")
		m.lastErr = err.Error()
	}
}

// podMetricsList mirrors the parts of metrics.k8s.io/v1beta1 PodMetricsList
// we read. Decoding it by hand avoids depending on k8s.io/metrics for two
// fields.
type podMetricsList struct {
	Items []struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Timestamp  time.Time `json:"timestamp"`
		Containers []struct {
			Name  string            `json:"name"`
			Usage map[string]string `json:"usage"`
		} `json:"containers"`
	} `json:"items"`
}

// gameContainerName is the container that runs the game (see buildPod).
const gameContainerName = "server"

func parsePodMetrics(raw []byte) ([]PodMetric, error) {
	var list podMetricsList
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	var out []PodMetric
	for _, it := range list.Items {
		for _, c := range it.Containers {
			if c.Name != gameContainerName {
				continue // skip the sftp-agent sidecar
			}
			cpu, err1 := resource.ParseQuantity(c.Usage["cpu"])
			mem, err2 := resource.ParseQuantity(c.Usage["memory"])
			if err1 != nil || err2 != nil {
				continue
			}
			out = append(out, PodMetric{
				Pod:           it.Metadata.Name,
				Timestamp:     it.Timestamp,
				CPUMillicores: cpu.MilliValue(),
				MemoryBytes:   mem.Value(),
			})
		}
	}
	return out, nil
}

func kubePodMetrics(clientset kubernetes.Interface) PodMetricsSource {
	return func(ctx context.Context, namespace string) ([]PodMetric, error) {
		raw, err := clientset.Discovery().RESTClient().Get().
			AbsPath("/apis/metrics.k8s.io/v1beta1/namespaces/"+namespace+"/pods").
			Param("labelSelector", gameserversv1alpha1.LabelGameServer).
			DoRaw(ctx)
		if err != nil {
			return nil, err
		}
		return parsePodMetrics(raw)
	}
}
