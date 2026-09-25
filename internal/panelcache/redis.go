package panelcache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const defaultKeyPrefix = "hatchery:"

// OpenRedis parses url (redis:// or rediss:// for TLS, password in the URL),
// connects and pings. The Panel refuses to boot without Redis: failing at
// startup beats discovering it the first time someone opens a console.
func OpenRedis(ctx context.Context, url string) (*redis.Client, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parsing redis url: %w", err)
	}
	rdb := redis.NewClient(opts)
	if err := rdb.Ping(ctx).Err(); err != nil {
		rdb.Close()
		return nil, fmt.Errorf("connecting to redis: %w", err)
	}
	return rdb, nil
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// RedisTicketStore keeps tickets in Redis under the SHA-256 of the ticket, so
// reading the keyspace never yields a usable ticket (the same reasoning as
// sessions in Postgres). Consume uses GETDEL (Redis >= 6.2), which reads and
// deletes atomically.
type RedisTicketStore struct {
	rdb    *redis.Client
	prefix string
}

func NewRedisTicketStore(rdb *redis.Client) *RedisTicketStore {
	return newRedisTicketStore(rdb, defaultKeyPrefix)
}

func newRedisTicketStore(rdb *redis.Client, prefix string) *RedisTicketStore {
	return &RedisTicketStore{rdb: rdb, prefix: prefix}
}

func (s *RedisTicketStore) key(ticket string) string {
	return s.prefix + "console-ticket:" + sha256Hex(ticket)
}

func (s *RedisTicketStore) Issue(ctx context.Context, t ConsoleTicket, ttl time.Duration) (string, error) {
	tok, err := newTicketValue()
	if err != nil {
		return "", err
	}
	val, err := json.Marshal(t)
	if err != nil {
		return "", err
	}
	if err := s.rdb.Set(ctx, s.key(tok), val, ttl).Err(); err != nil {
		return "", err
	}
	return tok, nil
}

func (s *RedisTicketStore) Consume(ctx context.Context, ticket string) (*ConsoleTicket, error) {
	raw, err := s.rdb.GetDel(ctx, s.key(ticket)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrTicketNotFound
	}
	if err != nil {
		return nil, err
	}
	var t ConsoleTicket
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// incrExpire increments KEYS[1] and, only when it was just created, sets its
// TTL — in one round trip, so a crash between the two can never leave a
// counter without an expiry.
var incrExpire = redis.NewScript(`
local n = redis.call('INCR', KEYS[1])
if n == 1 then redis.call('PEXPIRE', KEYS[1], ARGV[1]) end
return n`)

// RedisLoginLimiter implements LoginLimiter with two fixed-window counters.
type RedisLoginLimiter struct {
	rdb    *redis.Client
	prefix string
	limits LoginLimits
}

func NewRedisLoginLimiter(rdb *redis.Client, limits LoginLimits) *RedisLoginLimiter {
	return newRedisLoginLimiter(rdb, defaultKeyPrefix, limits)
}

func newRedisLoginLimiter(rdb *redis.Client, prefix string, limits LoginLimits) *RedisLoginLimiter {
	return &RedisLoginLimiter{rdb: rdb, prefix: prefix, limits: limits}
}

func (l *RedisLoginLimiter) ipKey(ip string) string {
	return l.prefix + "login-fail:ip:" + sha256Hex(ip)
}
func (l *RedisLoginLimiter) userIPKey(username, ip string) string {
	return l.prefix + "login-fail:user-ip:" + sha256Hex(strings.ToLower(username)+"|"+ip)
}

func (l *RedisLoginLimiter) Blocked(ctx context.Context, username, ip string) (bool, time.Duration, error) {
	for _, c := range []struct {
		key string
		max int64
	}{{l.ipKey(ip), l.limits.PerIP}, {l.userIPKey(username, ip), l.limits.PerUserIP}} {
		n, err := l.rdb.Get(ctx, c.key).Int64()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			return false, 0, err
		}
		if n >= c.max {
			ttl, err := l.rdb.PTTL(ctx, c.key).Result()
			if err != nil {
				return false, 0, err
			}
			if ttl <= 0 {
				ttl = l.limits.Window
			}
			return true, ttl, nil
		}
	}
	return false, 0, nil
}

func (l *RedisLoginLimiter) RecordFailure(ctx context.Context, username, ip string) error {
	window := l.limits.Window.Milliseconds()
	for _, key := range []string{l.ipKey(ip), l.userIPKey(username, ip)} {
		if err := incrExpire.Run(ctx, l.rdb, []string{key}, window).Err(); err != nil {
			return err
		}
	}
	return nil
}

func (l *RedisLoginLimiter) RecordSuccess(ctx context.Context, username, ip string) error {
	return l.rdb.Del(ctx, l.userIPKey(username, ip)).Err()
}

var (
	_ TicketStore    = (*RedisTicketStore)(nil)
	_ LoginLimiter   = (*RedisLoginLimiter)(nil)
	_ RequestLimiter = (*RedisRequestLimiter)(nil)
	_ TicketStore    = (*MemoryTicketStore)(nil)
	_ LoginLimiter   = (*MemoryLoginLimiter)(nil)
	_ RequestLimiter = (*MemoryRequestLimiter)(nil)
)
