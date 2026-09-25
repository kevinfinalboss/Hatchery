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
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/kevinfinalboss/Hatchery/internal/modsource"
)

// modHashTTL bounds how long hashes of a file nobody lists again stay around.
const modHashTTL = 7 * 24 * time.Hour

// ModHashKey identifies one version of a file: any change of size or modification time is a
// different key, so a replaced jar is hashed again.
type ModHashKey struct {
	Namespace, GameServer, Path string
	Size, ModTime               int64
}

func (k ModHashKey) String() string {
	return fmt.Sprintf("%s|%s|%s|%d|%d", k.Namespace, k.GameServer, k.Path, k.Size, k.ModTime)
}

// ModHashCache remembers the hashes of mod files, so listing a server's mods only reads new or
// changed jars through SFTP.
type ModHashCache interface {
	// Get returns nil, nil on a miss.
	Get(ctx context.Context, k ModHashKey) (*modsource.JarInfo, error)
	Set(ctx context.Context, k ModHashKey, h modsource.JarInfo) error
}

type RedisModHashCache struct {
	rdb    *redis.Client
	prefix string
}

func NewRedisModHashCache(rdb *redis.Client) *RedisModHashCache {
	return newRedisModHashCache(rdb, defaultKeyPrefix)
}

func newRedisModHashCache(rdb *redis.Client, prefix string) *RedisModHashCache {
	return &RedisModHashCache{rdb: rdb, prefix: prefix}
}

func (c *RedisModHashCache) key(k ModHashKey) string {
	// "modjar:" (not the earlier "modhash:") so entries cached before jar metadata existed are
	// not mistaken for jars that provide nothing.
	return c.prefix + "modjar:" + sha256Hex(k.String())
}

func (c *RedisModHashCache) Get(ctx context.Context, k ModHashKey) (*modsource.JarInfo, error) {
	raw, err := c.rdb.Get(ctx, c.key(k)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var h modsource.JarInfo
	if err := json.Unmarshal(raw, &h); err != nil {
		return nil, nil // unreadable entry: treat as a miss, it gets rewritten
	}
	return &h, nil
}

func (c *RedisModHashCache) Set(ctx context.Context, k ModHashKey, h modsource.JarInfo) error {
	raw, err := json.Marshal(h)
	if err != nil {
		return err
	}
	return c.rdb.Set(ctx, c.key(k), raw, modHashTTL).Err()
}

// MemoryModHashCache is an in-process ModHashCache for tests (no expiry).
type MemoryModHashCache struct {
	mu sync.Mutex
	m  map[string]modsource.JarInfo
}

func NewMemoryModHashCache() *MemoryModHashCache {
	return &MemoryModHashCache{m: map[string]modsource.JarInfo{}}
}

func (c *MemoryModHashCache) Get(_ context.Context, k ModHashKey) (*modsource.JarInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	h, ok := c.m[k.String()]
	if !ok {
		return nil, nil
	}
	return &h, nil
}

func (c *MemoryModHashCache) Set(_ context.Context, k ModHashKey, h modsource.JarInfo) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[k.String()] = h
	return nil
}

var (
	_ ModHashCache = (*RedisModHashCache)(nil)
	_ ModHashCache = (*MemoryModHashCache)(nil)
)
