/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package panelcache

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// gameVersionTTL bounds how long a server's last known game version is kept.
const gameVersionTTL = 30 * 24 * time.Hour

// GameVersionCache remembers the last game version read from a server's files, so the mods tab
// keeps working while the server restarts and its files are unreachable.
type GameVersionCache interface {
	// Get returns "" when nothing is known.
	Get(ctx context.Context, namespace, gameServer string) (string, error)
	Set(ctx context.Context, namespace, gameServer, version string) error
}

type RedisGameVersionCache struct {
	rdb    *redis.Client
	prefix string
}

func NewRedisGameVersionCache(rdb *redis.Client) *RedisGameVersionCache {
	return &RedisGameVersionCache{rdb: rdb, prefix: defaultKeyPrefix}
}

func (c *RedisGameVersionCache) key(ns, gs string) string {
	return c.prefix + "gameversion:" + ns + "/" + gs
}

func (c *RedisGameVersionCache) Get(ctx context.Context, ns, gs string) (string, error) {
	v, err := c.rdb.Get(ctx, c.key(ns, gs)).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	return v, err
}

func (c *RedisGameVersionCache) Set(ctx context.Context, ns, gs, v string) error {
	return c.rdb.Set(ctx, c.key(ns, gs), v, gameVersionTTL).Err()
}

// MemoryGameVersionCache is an in-process GameVersionCache for tests.
type MemoryGameVersionCache struct {
	mu sync.Mutex
	m  map[string]string
}

func NewMemoryGameVersionCache() *MemoryGameVersionCache {
	return &MemoryGameVersionCache{m: map[string]string{}}
}

func (c *MemoryGameVersionCache) Get(_ context.Context, ns, gs string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.m[ns+"/"+gs], nil
}

func (c *MemoryGameVersionCache) Set(_ context.Context, ns, gs, v string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[ns+"/"+gs] = v
	return nil
}

var (
	_ GameVersionCache = (*RedisGameVersionCache)(nil)
	_ GameVersionCache = (*MemoryGameVersionCache)(nil)
)
