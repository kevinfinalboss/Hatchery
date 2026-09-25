package panelcache

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// RequestLimiter counts every request against a key in fixed windows — unlike
// LoginLimiter, which only counts failures. Used for "forgot password", where
// each request sends an e-mail.
type RequestLimiter interface {
	// Allow counts one request against key and reports whether it is within
	// limit, and otherwise how long until the window ends.
	Allow(ctx context.Context, key string, limit int64, window time.Duration) (allowed bool, retryAfter time.Duration, err error)
}

// RedisRequestLimiter implements RequestLimiter with INCR + PEXPIRE (the same
// script as the login limiter). The key is hashed, so e-mails never sit in
// the keyspace in clear.
type RedisRequestLimiter struct {
	rdb    *redis.Client
	prefix string
}

func NewRedisRequestLimiter(rdb *redis.Client) *RedisRequestLimiter {
	return newRedisRequestLimiter(rdb, defaultKeyPrefix)
}

func newRedisRequestLimiter(rdb *redis.Client, prefix string) *RedisRequestLimiter {
	return &RedisRequestLimiter{rdb: rdb, prefix: prefix}
}

func (l *RedisRequestLimiter) Allow(ctx context.Context, key string, limit int64, window time.Duration) (bool, time.Duration, error) {
	k := l.prefix + "req:" + sha256Hex(key)
	n, err := incrExpire.Run(ctx, l.rdb, []string{k}, window.Milliseconds()).Int64()
	if err != nil {
		return false, 0, err
	}
	if n <= limit {
		return true, 0, nil
	}
	ttl, err := l.rdb.PTTL(ctx, k).Result()
	if err != nil {
		return false, 0, err
	}
	if ttl <= 0 {
		ttl = window
	}
	return false, ttl, nil
}

// MemoryRequestLimiter is an in-process RequestLimiter for tests.
type MemoryRequestLimiter struct {
	Now func() time.Time

	mu     sync.Mutex
	counts map[string]*memoryCounter
}

func NewMemoryRequestLimiter() *MemoryRequestLimiter {
	return &MemoryRequestLimiter{Now: time.Now, counts: map[string]*memoryCounter{}}
}

func (m *MemoryRequestLimiter) Allow(_ context.Context, key string, limit int64, window time.Duration) (bool, time.Duration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.Now()
	c := m.counts[key]
	if c == nil || !now.Before(c.expires) {
		c = &memoryCounter{expires: now.Add(window)}
		m.counts[key] = c
	}
	c.n++
	if c.n <= limit {
		return true, 0, nil
	}
	return false, c.expires.Sub(now), nil
}
