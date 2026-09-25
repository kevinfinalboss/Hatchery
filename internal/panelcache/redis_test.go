package panelcache

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func testRedis(t *testing.T) (*redis.Client, string) {
	t.Helper()
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("REDIS_TEST_ADDR not set, skipping Redis tests")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { rdb.Close() })
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("connecting to %s: %v", addr, err)
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return rdb, "hatchery-test-" + hex.EncodeToString(b) + ":"
}

func TestRedisTicketSingleUseAndStoresOnlyAHash(t *testing.T) {
	rdb, prefix := testRedis(t)
	s := newRedisTicketStore(rdb, prefix)
	ctx := context.Background()
	want := ConsoleTicket{UserID: 3, Org: "acme", GameServer: "mc"}

	tok, err := s.Issue(ctx, want, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	keys, _ := rdb.Keys(ctx, prefix+"*").Result()
	if len(keys) != 1 {
		t.Fatalf("expected exactly one key, got %v", keys)
	}
	for _, k := range keys {
		if len(k) >= len(tok) && k[len(k)-len(tok):] == tok {
			t.Fatal("the raw ticket must never be a Redis key: only its hash may be stored")
		}
	}
	if ttl, _ := rdb.TTL(ctx, keys[0]).Result(); ttl <= 0 || ttl > 30*time.Second {
		t.Errorf("key TTL = %v, want within (0, 30s]", ttl)
	}

	got, err := s.Consume(ctx, tok)
	if err != nil || *got != want {
		t.Fatalf("Consume = %v, %v", got, err)
	}
	if _, err := s.Consume(ctx, tok); !errors.Is(err, ErrTicketNotFound) {
		t.Fatalf("second Consume: got %v, want ErrTicketNotFound", err)
	}
}

func TestRedisTicketExpires(t *testing.T) {
	rdb, prefix := testRedis(t)
	s := newRedisTicketStore(rdb, prefix)
	tok, _ := s.Issue(context.Background(), ConsoleTicket{UserID: 1}, 100*time.Millisecond)
	time.Sleep(250 * time.Millisecond)
	if _, err := s.Consume(context.Background(), tok); !errors.Is(err, ErrTicketNotFound) {
		t.Fatalf("expired ticket: got %v, want ErrTicketNotFound", err)
	}
}

func TestRedisTicketConcurrentConsumeHasOneWinner(t *testing.T) {
	rdb, prefix := testRedis(t)
	s := newRedisTicketStore(rdb, prefix)
	ctx := context.Background()
	tok, _ := s.Issue(ctx, ConsoleTicket{UserID: 1}, 30*time.Second)

	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Consume(ctx, tok); err == nil {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("%d concurrent consumers succeeded, want exactly 1", wins.Load())
	}
}

func TestRedisLoginLimiter(t *testing.T) {
	rdb, prefix := testRedis(t)
	l := newRedisLoginLimiter(rdb, prefix, LoginLimits{PerIP: 20, PerUserIP: 5, Window: 10 * time.Second})
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if b, _, err := l.Blocked(ctx, "Kevin", "1.1.1.1"); err != nil || b {
			t.Fatalf("attempt %d: blocked=%v err=%v", i, b, err)
		}
		if err := l.RecordFailure(ctx, "Kevin", "1.1.1.1"); err != nil {
			t.Fatal(err)
		}
	}
	blocked, retry, err := l.Blocked(ctx, "kevin", "1.1.1.1") // username is case-insensitive
	if err != nil || !blocked || retry <= 0 || retry > 10*time.Second {
		t.Fatalf("blocked=%v retry=%v err=%v", blocked, retry, err)
	}
	if b, _, _ := l.Blocked(ctx, "kevin", "2.2.2.2"); b {
		t.Error("another IP must not be blocked")
	}

	keys, _ := rdb.Keys(ctx, prefix+"*").Result()
	for _, k := range keys {
		if ttl, _ := rdb.TTL(ctx, k).Result(); ttl <= 0 {
			t.Errorf("key %s has no TTL: a counter must never outlive its window", k)
		}
	}

	if err := l.RecordSuccess(ctx, "kevin", "1.1.1.1"); err != nil {
		t.Fatal(err)
	}
	if b, _, _ := l.Blocked(ctx, "kevin", "1.1.1.1"); b {
		t.Error("success must reset the user+IP counter")
	}
}

func TestRedisMetricsStoreAppendIsIdempotentAndRangeFilters(t *testing.T) {
	rdb, prefix := testRedis(t)
	s := newRedisMetricsStore(rdb, prefix)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	old := MetricSample{T: now - 3600_000, CPUMillicores: 1, MemoryBytes: 1}
	recent := MetricSample{T: now - 1000, CPUMillicores: 250, MemoryBytes: 1 << 30}
	for _, smp := range []MetricSample{old, recent, recent} {
		if err := s.Append(ctx, "ns", "mc", smp); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Range(ctx, "ns", "mc", now-60_000)
	if err != nil || len(got) != 1 || got[0] != recent {
		t.Fatalf("Range = %+v, %v; want just %+v", got, err, recent)
	}
	if got, _ := s.Range(ctx, "ns", "other", 0); len(got) != 0 {
		t.Fatalf("series must be per server, got %+v", got)
	}
}

func TestRedisRequestLimiter(t *testing.T) {
	rdb, prefix := testRedis(t)
	l := newRedisRequestLimiter(rdb, prefix)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if ok, _, err := l.Allow(ctx, "k", 2, time.Minute); err != nil || !ok {
			t.Fatalf("request %d: ok=%v err=%v", i+1, ok, err)
		}
	}
	ok, retry, err := l.Allow(ctx, "k", 2, time.Minute)
	if err != nil || ok || retry <= 0 || retry > time.Minute {
		t.Fatalf("3rd: ok=%v retry=%v err=%v", ok, retry, err)
	}
}

func TestRedisModHashCache(t *testing.T) {
	rdb, prefix := testRedis(t)
	testModHashCache(t, newRedisModHashCache(rdb, prefix))
}
