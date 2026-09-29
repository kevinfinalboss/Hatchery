package panelcache

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrChallengeNotFound means the login challenge or pending secret does not exist, expired, was
// used, or died after too many failures — deliberately indistinguishable.
var ErrChallengeNotFound = errors.New("login challenge not found or expired")

// LoginChallengeStore holds the second step of a two-factor login: a single-use token that stands
// for "this user got the password right", with a failure counter.
type LoginChallengeStore interface {
	// Issue returns a new opaque token for userID, valid for ttl.
	Issue(ctx context.Context, userID int64, ttl time.Duration) (string, error)
	// Peek returns the user without consuming the token.
	Peek(ctx context.Context, token string) (int64, error)
	// Fail counts a wrong code; at max failures the token is deleted and dead is true (also true
	// for a token that no longer exists).
	Fail(ctx context.Context, token string, max int) (dead bool, err error)
	// Consume returns the user and deletes the token atomically.
	Consume(ctx context.Context, token string) (int64, error)
}

// PendingSecretStore keeps the (encrypted) TOTP secret between setup and the confirming code.
type PendingSecretStore interface {
	Put(ctx context.Context, userID int64, secretEnc string, ttl time.Duration) error
	Get(ctx context.Context, userID int64) (string, error)
	Delete(ctx context.Context, userID int64) error
}

// RedisLoginChallengeStore keeps each challenge as a hash (u = user, f = failures) under the
// SHA-256 of the token, so reading the keyspace never yields a usable token.
type RedisLoginChallengeStore struct {
	rdb    *redis.Client
	prefix string
}

func NewRedisLoginChallengeStore(rdb *redis.Client) *RedisLoginChallengeStore {
	return newRedisLoginChallengeStore(rdb, defaultKeyPrefix)
}

func newRedisLoginChallengeStore(rdb *redis.Client, prefix string) *RedisLoginChallengeStore {
	return &RedisLoginChallengeStore{rdb: rdb, prefix: prefix}
}

func (s *RedisLoginChallengeStore) key(token string) string {
	return s.prefix + "2fa-challenge:" + sha256Hex(token)
}

func (s *RedisLoginChallengeStore) Issue(ctx context.Context, userID int64, ttl time.Duration) (string, error) {
	tok, err := newTicketValue()
	if err != nil {
		return "", err
	}
	k := s.key(tok)
	pipe := s.rdb.TxPipeline()
	pipe.HSet(ctx, k, "u", userID, "f", 0)
	pipe.PExpire(ctx, k, ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return "", err
	}
	return tok, nil
}

func (s *RedisLoginChallengeStore) Peek(ctx context.Context, token string) (int64, error) {
	id, err := s.rdb.HGet(ctx, s.key(token), "u").Int64()
	if errors.Is(err, redis.Nil) {
		return 0, ErrChallengeNotFound
	}
	return id, err
}

// challengeFail counts a failure and deletes the challenge at ARGV[1]; -1 when it does not exist.
var challengeFail = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then return -1 end
local n = redis.call('HINCRBY', KEYS[1], 'f', 1)
if n >= tonumber(ARGV[1]) then redis.call('DEL', KEYS[1]) end
return n`)

func (s *RedisLoginChallengeStore) Fail(ctx context.Context, token string, max int) (bool, error) {
	n, err := challengeFail.Run(ctx, s.rdb, []string{s.key(token)}, max).Int()
	if err != nil {
		return false, err
	}
	return n < 0 || n >= max, nil
}

// challengeConsume reads the user and deletes the challenge in one step.
var challengeConsume = redis.NewScript(`
local u = redis.call('HGET', KEYS[1], 'u')
if u then redis.call('DEL', KEYS[1]) end
return u`)

func (s *RedisLoginChallengeStore) Consume(ctx context.Context, token string) (int64, error) {
	v, err := challengeConsume.Run(ctx, s.rdb, []string{s.key(token)}).Text()
	if errors.Is(err, redis.Nil) {
		return 0, ErrChallengeNotFound
	}
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(v, 10, 64)
}

// RedisPendingSecretStore keeps one pending secret per user.
type RedisPendingSecretStore struct {
	rdb    *redis.Client
	prefix string
}

func NewRedisPendingSecretStore(rdb *redis.Client) *RedisPendingSecretStore {
	return newRedisPendingSecretStore(rdb, defaultKeyPrefix)
}

func newRedisPendingSecretStore(rdb *redis.Client, prefix string) *RedisPendingSecretStore {
	return &RedisPendingSecretStore{rdb: rdb, prefix: prefix}
}

func (s *RedisPendingSecretStore) key(userID int64) string {
	return s.prefix + "2fa-pending:" + strconv.FormatInt(userID, 10)
}

func (s *RedisPendingSecretStore) Put(ctx context.Context, userID int64, secretEnc string, ttl time.Duration) error {
	return s.rdb.Set(ctx, s.key(userID), secretEnc, ttl).Err()
}

func (s *RedisPendingSecretStore) Get(ctx context.Context, userID int64) (string, error) {
	v, err := s.rdb.Get(ctx, s.key(userID)).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrChallengeNotFound
	}
	return v, err
}

func (s *RedisPendingSecretStore) Delete(ctx context.Context, userID int64) error {
	return s.rdb.Del(ctx, s.key(userID)).Err()
}

// MemoryLoginChallengeStore is an in-process LoginChallengeStore for tests.
type MemoryLoginChallengeStore struct {
	Now func() time.Time

	mu    sync.Mutex
	items map[string]*memoryChallenge
}

type memoryChallenge struct {
	userID   int64
	failures int
	expires  time.Time
}

func NewMemoryLoginChallengeStore() *MemoryLoginChallengeStore {
	return &MemoryLoginChallengeStore{Now: time.Now, items: map[string]*memoryChallenge{}}
}

// live returns the challenge when it exists and has not expired; the caller holds mu.
func (m *MemoryLoginChallengeStore) live(token string) *memoryChallenge {
	c, ok := m.items[token]
	if !ok || !m.Now().Before(c.expires) {
		delete(m.items, token)
		return nil
	}
	return c
}

func (m *MemoryLoginChallengeStore) Issue(_ context.Context, userID int64, ttl time.Duration) (string, error) {
	tok, err := newTicketValue()
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[tok] = &memoryChallenge{userID: userID, expires: m.Now().Add(ttl)}
	return tok, nil
}

func (m *MemoryLoginChallengeStore) Peek(_ context.Context, token string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c := m.live(token); c != nil {
		return c.userID, nil
	}
	return 0, ErrChallengeNotFound
}

func (m *MemoryLoginChallengeStore) Fail(_ context.Context, token string, max int) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.live(token)
	if c == nil {
		return true, nil
	}
	c.failures++
	if c.failures >= max {
		delete(m.items, token)
		return true, nil
	}
	return false, nil
}

func (m *MemoryLoginChallengeStore) Consume(_ context.Context, token string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.live(token)
	if c == nil {
		return 0, ErrChallengeNotFound
	}
	delete(m.items, token)
	return c.userID, nil
}

// MemoryPendingSecretStore is an in-process PendingSecretStore for tests.
type MemoryPendingSecretStore struct {
	Now func() time.Time

	mu    sync.Mutex
	items map[int64]memoryPending
}

type memoryPending struct {
	secret  string
	expires time.Time
}

func NewMemoryPendingSecretStore() *MemoryPendingSecretStore {
	return &MemoryPendingSecretStore{Now: time.Now, items: map[int64]memoryPending{}}
}

func (m *MemoryPendingSecretStore) Put(_ context.Context, userID int64, secretEnc string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[userID] = memoryPending{secret: secretEnc, expires: m.Now().Add(ttl)}
	return nil
}

func (m *MemoryPendingSecretStore) Get(_ context.Context, userID int64) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.items[userID]
	if !ok || !m.Now().Before(p.expires) {
		delete(m.items, userID)
		return "", ErrChallengeNotFound
	}
	return p.secret, nil
}

func (m *MemoryPendingSecretStore) Delete(_ context.Context, userID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.items, userID)
	return nil
}
