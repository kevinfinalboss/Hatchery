package panelcache

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const MetricsRetention = 7 * time.Hour

type MetricSample struct {
	T             int64
	CPUMillicores int64
	MemoryBytes   int64
}

type MetricsStore interface {
	Append(ctx context.Context, namespace, name string, s MetricSample) error
	Range(ctx context.Context, namespace, name string, sinceMs int64) ([]MetricSample, error)
}

type MemoryMetricsStore struct {
	Now func() time.Time

	mu     sync.Mutex
	series map[string]map[int64]MetricSample
}

func NewMemoryMetricsStore() *MemoryMetricsStore {
	return &MemoryMetricsStore{Now: time.Now, series: map[string]map[int64]MetricSample{}}
}

func (m *MemoryMetricsStore) Append(_ context.Context, namespace, name string, s MetricSample) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := namespace + "/" + name
	if m.series[key] == nil {
		m.series[key] = map[int64]MetricSample{}
	}
	m.series[key][s.T] = s
	cutoff := m.Now().Add(-MetricsRetention).UnixMilli()
	for t := range m.series[key] {
		if t < cutoff {
			delete(m.series[key], t)
		}
	}
	return nil
}

func (m *MemoryMetricsStore) Range(_ context.Context, namespace, name string, sinceMs int64) ([]MetricSample, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []MetricSample
	for _, s := range m.series[namespace+"/"+name] {
		if s.T >= sinceMs {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].T < out[j].T })
	return out, nil
}

type RedisMetricsStore struct {
	rdb    *redis.Client
	prefix string
}

func NewRedisMetricsStore(rdb *redis.Client) *RedisMetricsStore {
	return newRedisMetricsStore(rdb, defaultKeyPrefix)
}

func newRedisMetricsStore(rdb *redis.Client, prefix string) *RedisMetricsStore {
	return &RedisMetricsStore{rdb: rdb, prefix: prefix}
}

func (s *RedisMetricsStore) key(namespace, name string) string {
	return s.prefix + "metrics:" + namespace + "/" + name
}

func (s *RedisMetricsStore) Append(ctx context.Context, namespace, name string, sample MetricSample) error {
	key := s.key(namespace, name)
	member := fmt.Sprintf("%d|%d|%d", sample.T, sample.CPUMillicores, sample.MemoryBytes)
	cutoff := time.Now().Add(-MetricsRetention).UnixMilli()

	pipe := s.rdb.Pipeline()
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(sample.T), Member: member})
	pipe.ZRemRangeByScore(ctx, key, "-inf", strconv.FormatInt(cutoff-1, 10))
	pipe.Expire(ctx, key, MetricsRetention)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *RedisMetricsStore) Range(ctx context.Context, namespace, name string, sinceMs int64) ([]MetricSample, error) {
	members, err := s.rdb.ZRangeByScore(ctx, s.key(namespace, name), &redis.ZRangeBy{
		Min: strconv.FormatInt(sinceMs, 10),
		Max: "+inf",
	}).Result()
	if err != nil {
		return nil, err
	}
	out := make([]MetricSample, 0, len(members))
	for _, m := range members {
		parts := strings.Split(m, "|")
		if len(parts) != 3 {
			continue
		}
		t, e1 := strconv.ParseInt(parts[0], 10, 64)
		cpu, e2 := strconv.ParseInt(parts[1], 10, 64)
		mem, e3 := strconv.ParseInt(parts[2], 10, 64)
		if e1 != nil || e2 != nil || e3 != nil {
			continue
		}
		out = append(out, MetricSample{T: t, CPUMillicores: cpu, MemoryBytes: mem})
	}
	return out, nil
}
