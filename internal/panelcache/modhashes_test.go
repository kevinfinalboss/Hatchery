package panelcache

import (
	"context"
	"testing"

	"github.com/kevinfinalboss/Hatchery/internal/modsource"
)

func testModHashCache(t *testing.T, c ModHashCache) {
	t.Helper()
	ctx := context.Background()
	k := ModHashKey{Namespace: "hatchery-acme", GameServer: "mc", Path: "plugins/a.jar", Size: 10, ModTime: 100}
	if h, err := c.Get(ctx, k); err != nil || h != nil {
		t.Fatalf("empty cache: %v %v", h, err)
	}
	want := modsource.JarInfo{Hashes: modsource.FileHashes{SHA1: "s1", SHA512: "s5", Fingerprint: 42}, Meta: modsource.JarMeta{Provides: []string{"spark"}}}
	if err := c.Set(ctx, k, want); err != nil {
		t.Fatal(err)
	}
	if h, err := c.Get(ctx, k); err != nil || h == nil || h.Hashes != want.Hashes || len(h.Meta.Provides) != 1 {
		t.Fatalf("hit: %v %v", h, err)
	}
	changed := k
	changed.ModTime = 101
	if h, _ := c.Get(ctx, changed); h != nil {
		t.Fatal("a changed mtime must miss")
	}
	changed = k
	changed.Size = 11
	if h, _ := c.Get(ctx, changed); h != nil {
		t.Fatal("a changed size must miss")
	}
}
