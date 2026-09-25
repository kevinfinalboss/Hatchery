package panelcache

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryTicketSingleUse(t *testing.T) {
	s := NewMemoryTicketStore()
	ctx := context.Background()
	want := ConsoleTicket{UserID: 7, Org: "acme", GameServer: "mc"}

	tok, err := s.Issue(ctx, want, 30*time.Second)
	if err != nil || tok == "" {
		t.Fatalf("Issue = %q, %v", tok, err)
	}
	got, err := s.Consume(ctx, tok)
	if err != nil || *got != want {
		t.Fatalf("Consume = %v, %v; want %v", got, err, want)
	}
	if _, err := s.Consume(ctx, tok); !errors.Is(err, ErrTicketNotFound) {
		t.Fatalf("second Consume: got %v, want ErrTicketNotFound", err)
	}
}

func TestMemoryTicketExpires(t *testing.T) {
	s := NewMemoryTicketStore()
	now := time.Now()
	s.Now = func() time.Time { return now }
	tok, _ := s.Issue(context.Background(), ConsoleTicket{UserID: 1}, 30*time.Second)

	now = now.Add(31 * time.Second)
	if _, err := s.Consume(context.Background(), tok); !errors.Is(err, ErrTicketNotFound) {
		t.Fatalf("expired ticket: got %v, want ErrTicketNotFound", err)
	}
}

func TestMemoryTicketsAreUnpredictable(t *testing.T) {
	s := NewMemoryTicketStore()
	a, _ := s.Issue(context.Background(), ConsoleTicket{}, time.Minute)
	b, _ := s.Issue(context.Background(), ConsoleTicket{}, time.Minute)
	if a == b || len(a) < 40 {
		t.Fatalf("tickets must be long and distinct, got %q and %q", a, b)
	}
}

func TestMemoryLoginLimiter(t *testing.T) {
	l := NewMemoryLoginLimiter(LoginLimits{PerIP: 20, PerUserIP: 5, Window: 15 * time.Minute})
	now := time.Now()
	l.Now = func() time.Time { return now }
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if b, _, _ := l.Blocked(ctx, "kevin", "1.1.1.1"); b {
			t.Fatalf("blocked after only %d failures", i)
		}
		_ = l.RecordFailure(ctx, "kevin", "1.1.1.1")
	}
	blocked, retry, err := l.Blocked(ctx, "kevin", "1.1.1.1")
	if err != nil || !blocked || retry <= 0 || retry > 15*time.Minute {
		t.Fatalf("after 5 failures: blocked=%v retry=%v err=%v", blocked, retry, err)
	}

	if b, _, _ := l.Blocked(ctx, "kevin", "2.2.2.2"); b {
		t.Error("a different IP must not be blocked by the user+IP counter")
	}
	if b, _, _ := l.Blocked(ctx, "someone-else", "1.1.1.1"); b {
		t.Error("a different username from the same IP must not be blocked by the user+IP counter")
	}

	_ = l.RecordSuccess(ctx, "kevin", "1.1.1.1")
	if b, _, _ := l.Blocked(ctx, "kevin", "1.1.1.1"); b {
		t.Error("a successful login must reset the user+IP counter")
	}

	now = now.Add(16 * time.Minute)
	for i := 0; i < 5; i++ {
		_ = l.RecordFailure(ctx, "kevin", "1.1.1.1")
	}
	now = now.Add(16 * time.Minute)
	if b, _, _ := l.Blocked(ctx, "kevin", "1.1.1.1"); b {
		t.Error("the block must lift once the window has passed")
	}
}

func TestMemoryLoginLimiterPerIPCeiling(t *testing.T) {
	l := NewMemoryLoginLimiter(LoginLimits{PerIP: 20, PerUserIP: 5, Window: time.Hour})
	ctx := context.Background()
	for i := 0; i < 20; i++ {
		_ = l.RecordFailure(ctx, "user"+string(rune('a'+i)), "9.9.9.9") // 20 distinct usernames, same IP
	}
	if b, _, _ := l.Blocked(ctx, "fresh-user", "9.9.9.9"); !b {
		t.Fatal("20 failures from one IP must block that IP even for a username it never tried")
	}
}

func TestMemoryRequestLimiterFixedWindow(t *testing.T) {
	now := time.Unix(1000, 0)
	l := NewMemoryRequestLimiter()
	l.Now = func() time.Time { return now }
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if ok, _, _ := l.Allow(ctx, "k", 3, time.Hour); !ok {
			t.Fatalf("request %d refused", i+1)
		}
	}
	ok, retry, _ := l.Allow(ctx, "k", 3, time.Hour)
	if ok || retry <= 0 || retry > time.Hour {
		t.Fatalf("4th: ok=%v retry=%v", ok, retry)
	}
	if ok, _, _ := l.Allow(ctx, "other", 3, time.Hour); !ok {
		t.Fatal("keys must be independent")
	}
	now = now.Add(time.Hour)
	if ok, _, _ := l.Allow(ctx, "k", 3, time.Hour); !ok {
		t.Fatal("window did not reset")
	}
}
