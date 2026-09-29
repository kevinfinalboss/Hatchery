package panelcache

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// NotifiedTTL is how long an alert stays claimed: far longer than the notifier's 15-minute
// window, so the same occurrence is never announced twice.
const NotifiedTTL = 24 * time.Hour

// NotifiedStore remembers which alerts were already sent. Claim returns true only for the first
// caller with a given identity, across Panel replicas.
type NotifiedStore interface {
	Claim(ctx context.Context, identity string) (bool, error)
}

// MemoryNotifiedStore is an in-process NotifiedStore for tests.
type MemoryNotifiedStore struct {
	mu      sync.Mutex
	claimed map[string]bool
}

func NewMemoryNotifiedStore() *MemoryNotifiedStore {
	return &MemoryNotifiedStore{claimed: map[string]bool{}}
}

func (m *MemoryNotifiedStore) Claim(_ context.Context, identity string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.claimed[identity] {
		return false, nil
	}
	m.claimed[identity] = true
	return true, nil
}

type RedisNotifiedStore struct {
	rdb    *redis.Client
	prefix string
}

func NewRedisNotifiedStore(rdb *redis.Client) *RedisNotifiedStore {
	return newRedisNotifiedStore(rdb, defaultKeyPrefix)
}

func newRedisNotifiedStore(rdb *redis.Client, prefix string) *RedisNotifiedStore {
	return &RedisNotifiedStore{rdb: rdb, prefix: prefix}
}

func (s *RedisNotifiedStore) Claim(ctx context.Context, identity string) (bool, error) {
	return s.rdb.SetNX(ctx, s.prefix+"notified:"+identity, 1, NotifiedTTL).Result()
}
