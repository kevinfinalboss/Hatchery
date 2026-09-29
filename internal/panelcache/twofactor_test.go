package panelcache

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func testChallengeStore(t *testing.T, s LoginChallengeStore, expire func()) {
	ctx := context.Background()
	tok, err := s.Issue(ctx, 42, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if id, err := s.Peek(ctx, tok); err != nil || id != 42 {
		t.Fatalf("Peek = %d, %v", id, err)
	}
	if id, err := s.Consume(ctx, tok); err != nil || id != 42 {
		t.Fatalf("Consume = %d, %v", id, err)
	}
	if _, err := s.Consume(ctx, tok); !errors.Is(err, ErrChallengeNotFound) {
		t.Fatalf("second Consume = %v", err)
	}
	if _, err := s.Peek(ctx, tok); !errors.Is(err, ErrChallengeNotFound) {
		t.Fatalf("Peek after Consume = %v", err)
	}

	// Five failures kill the challenge.
	tok, _ = s.Issue(ctx, 7, time.Minute)
	for i := 1; i <= 4; i++ {
		if dead, err := s.Fail(ctx, tok, 5); err != nil || dead {
			t.Fatalf("failure %d: dead=%v err=%v", i, dead, err)
		}
	}
	if dead, err := s.Fail(ctx, tok, 5); err != nil || !dead {
		t.Fatalf("5th failure: dead=%v err=%v", dead, err)
	}
	if _, err := s.Peek(ctx, tok); !errors.Is(err, ErrChallengeNotFound) {
		t.Fatalf("Peek after 5 failures = %v", err)
	}
	if dead, err := s.Fail(ctx, "unknown", 5); err != nil || !dead {
		t.Fatalf("Fail on unknown: dead=%v err=%v", dead, err)
	}

	tok, _ = s.Issue(ctx, 9, time.Minute)
	expire()
	if _, err := s.Peek(ctx, tok); !errors.Is(err, ErrChallengeNotFound) {
		t.Fatalf("Peek after expiry = %v", err)
	}
}

func testPendingSecretStore(t *testing.T, s PendingSecretStore, expire func()) {
	ctx := context.Background()
	if _, err := s.Get(ctx, 1); !errors.Is(err, ErrChallengeNotFound) {
		t.Fatalf("Get without Put = %v", err)
	}
	if err := s.Put(ctx, 1, "enc", time.Minute); err != nil {
		t.Fatal(err)
	}
	if v, err := s.Get(ctx, 1); err != nil || v != "enc" {
		t.Fatalf("Get = %q, %v", v, err)
	}
	if err := s.Delete(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, 1); !errors.Is(err, ErrChallengeNotFound) {
		t.Fatalf("Get after Delete = %v", err)
	}
	_ = s.Put(ctx, 1, "enc", time.Minute)
	expire()
	if _, err := s.Get(ctx, 1); !errors.Is(err, ErrChallengeNotFound) {
		t.Fatalf("Get after expiry = %v", err)
	}
}

func TestMemoryTwoFactorStores(t *testing.T) {
	now := time.Unix(1000, 0)
	c := NewMemoryLoginChallengeStore()
	c.Now = func() time.Time { return now }
	testChallengeStore(t, c, func() { now = now.Add(2 * time.Minute) })
	p := NewMemoryPendingSecretStore()
	p.Now = func() time.Time { return now }
	testPendingSecretStore(t, p, func() { now = now.Add(2 * time.Minute) })
}

func TestRedisTwoFactorStores(t *testing.T) {
	rdb, prefix := testRedis(t)
	c := newRedisLoginChallengeStore(rdb, prefix)
	p := newRedisPendingSecretStore(rdb, prefix)
	ctx := context.Background()
	flush := func() {
		keys, _ := rdb.Keys(ctx, prefix+"*").Result()
		for _, k := range keys {
			rdb.Del(ctx, k)
		}
	}
	testChallengeStore(t, c, flush)
	testPendingSecretStore(t, p, flush)

	// Only the hash of the challenge token is a key.
	tok, _ := c.Issue(ctx, 1, time.Minute)
	keys, _ := rdb.Keys(ctx, prefix+"*").Result()
	for _, k := range keys {
		if strings.Contains(k, tok) {
			t.Fatalf("key %q contains the raw token", k)
		}
	}
}
