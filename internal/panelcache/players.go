package panelcache

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// PlayersTTL is three sampler rounds: one failed query doesn't blank the count, and a
// server that stopped answering disappears within a minute.
const PlayersTTL = 45 * time.Second

// PlayersSnapshot is the last successful status query of one server.
type PlayersSnapshot struct {
	Online  int       `json:"online"`
	Max     int       `json:"max"`
	Players []string  `json:"players,omitempty"`
	At      time.Time `json:"at"`
}

// PlayersStore keeps the latest PlayersSnapshot per server for PlayersTTL.
type PlayersStore interface {
	Set(ctx context.Context, namespace, name string, s PlayersSnapshot) error
	// GetMany returns only the servers that have a live snapshot.
	GetMany(ctx context.Context, namespace string, names []string) (map[string]PlayersSnapshot, error)
}

type memoryPlayers struct {
	s       PlayersSnapshot
	expires time.Time
}

// MemoryPlayersStore is an in-process PlayersStore for tests.
type MemoryPlayersStore struct {
	Now func() time.Time

	mu   sync.Mutex
	data map[string]memoryPlayers
}

func NewMemoryPlayersStore() *MemoryPlayersStore {
	return &MemoryPlayersStore{Now: time.Now, data: map[string]memoryPlayers{}}
}

func (m *MemoryPlayersStore) Set(_ context.Context, namespace, name string, s PlayersSnapshot) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[namespace+"/"+name] = memoryPlayers{s: s, expires: m.Now().Add(PlayersTTL)}
	return nil
}

func (m *MemoryPlayersStore) GetMany(_ context.Context, namespace string, names []string) (map[string]PlayersSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]PlayersSnapshot{}
	for _, n := range names {
		if e, ok := m.data[namespace+"/"+n]; ok && m.Now().Before(e.expires) {
			out[n] = e.s
		}
	}
	return out, nil
}

type RedisPlayersStore struct {
	rdb    *redis.Client
	prefix string
}

func NewRedisPlayersStore(rdb *redis.Client) *RedisPlayersStore {
	return newRedisPlayersStore(rdb, defaultKeyPrefix)
}

func newRedisPlayersStore(rdb *redis.Client, prefix string) *RedisPlayersStore {
	return &RedisPlayersStore{rdb: rdb, prefix: prefix}
}

func (s *RedisPlayersStore) key(namespace, name string) string {
	return s.prefix + "players:" + namespace + "/" + name
}

func (s *RedisPlayersStore) Set(ctx context.Context, namespace, name string, snap PlayersSnapshot) error {
	raw, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	return s.rdb.Set(ctx, s.key(namespace, name), raw, PlayersTTL).Err()
}

func (s *RedisPlayersStore) GetMany(ctx context.Context, namespace string, names []string) (map[string]PlayersSnapshot, error) {
	out := map[string]PlayersSnapshot{}
	if len(names) == 0 {
		return out, nil
	}
	keys := make([]string, len(names))
	for i, n := range names {
		keys[i] = s.key(namespace, n)
	}
	vals, err := s.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	for i, v := range vals {
		str, ok := v.(string)
		if !ok {
			continue // missing or expired
		}
		var snap PlayersSnapshot
		if json.Unmarshal([]byte(str), &snap) == nil {
			out[names[i]] = snap
		}
	}
	return out, nil
}
