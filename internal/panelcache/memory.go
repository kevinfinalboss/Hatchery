package panelcache

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"sync"
	"time"
)

// newTicketValue returns 32 random bytes as base64url, the ticket format
// shared by every TicketStore implementation.
func newTicketValue() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// MemoryTicketStore is an in-process TicketStore for tests.
type MemoryTicketStore struct {
	Now func() time.Time

	mu      sync.Mutex
	tickets map[string]memoryTicket
}

type memoryTicket struct {
	t       ConsoleTicket
	expires time.Time
}

func NewMemoryTicketStore() *MemoryTicketStore {
	return &MemoryTicketStore{Now: time.Now, tickets: map[string]memoryTicket{}}
}

func (m *MemoryTicketStore) Issue(_ context.Context, t ConsoleTicket, ttl time.Duration) (string, error) {
	tok, err := newTicketValue()
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tickets[tok] = memoryTicket{t: t, expires: m.Now().Add(ttl)}
	return tok, nil
}

func (m *MemoryTicketStore) Consume(_ context.Context, ticket string) (*ConsoleTicket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	mt, ok := m.tickets[ticket]
	delete(m.tickets, ticket)
	if !ok || !m.Now().Before(mt.expires) {
		return nil, ErrTicketNotFound
	}
	return &mt.t, nil
}

// MemoryLoginLimiter is an in-process LoginLimiter for tests.
type MemoryLoginLimiter struct {
	Now func() time.Time

	limits LoginLimits
	mu     sync.Mutex
	counts map[string]*memoryCounter
}

type memoryCounter struct {
	n       int64
	expires time.Time
}

func NewMemoryLoginLimiter(l LoginLimits) *MemoryLoginLimiter {
	return &MemoryLoginLimiter{Now: time.Now, limits: l, counts: map[string]*memoryCounter{}}
}

func ipKey(ip string) string               { return "ip:" + ip }
func userIPKey(username, ip string) string { return "userip:" + strings.ToLower(username) + "|" + ip }

func (m *MemoryLoginLimiter) live(key string) *memoryCounter {
	c := m.counts[key]
	if c != nil && !m.Now().Before(c.expires) {
		delete(m.counts, key)
		return nil
	}
	return c
}

func (m *MemoryLoginLimiter) Blocked(_ context.Context, username, ip string) (bool, time.Duration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range []struct {
		key string
		max int64
	}{{ipKey(ip), m.limits.PerIP}, {userIPKey(username, ip), m.limits.PerUserIP}} {
		if c := m.live(k.key); c != nil && c.n >= k.max {
			return true, c.expires.Sub(m.Now()), nil
		}
	}
	return false, 0, nil
}

func (m *MemoryLoginLimiter) RecordFailure(_ context.Context, username, ip string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, key := range []string{ipKey(ip), userIPKey(username, ip)} {
		c := m.live(key)
		if c == nil {
			c = &memoryCounter{expires: m.Now().Add(m.limits.Window)}
			m.counts[key] = c
		}
		c.n++
	}
	return nil
}

func (m *MemoryLoginLimiter) RecordSuccess(_ context.Context, username, ip string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.counts, userIPKey(username, ip))
	return nil
}
