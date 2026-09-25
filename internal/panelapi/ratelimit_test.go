package panelapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func login(t *testing.T, srv *Server, remote, xff, username, password string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(loginRequest{Username: username, Password: password})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.RemoteAddr = remote
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	return rec
}

func TestLoginIsBlockedAfterRepeatedFailuresEvenWithTheRightPassword(t *testing.T) {
	srv := newTestServer(t)
	if _, err := srv.DB.CreateUser(t.Context(), "victim", "victim@example.com", "right-password", false); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 5; i++ {
		if rec := login(t, srv, "203.0.113.5:1000", "", "victim", "wrong"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d: got %d, want 401", i+1, rec.Code)
		}
	}
	rec := login(t, srv, "203.0.113.5:1000", "", "victim", "right-password")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("after 5 failures even the correct password must be refused, got %d", rec.Code)
	}
	secs, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	if err != nil || secs <= 0 || secs > 15*60 {
		t.Fatalf("Retry-After = %q, want a positive integer number of seconds within the window", rec.Header().Get("Retry-After"))
	}
}

func TestBlockedIPDoesNotLockOutTheRealUserFromAnotherIP(t *testing.T) {
	srv := newTestServer(t)
	if _, err := srv.DB.CreateUser(t.Context(), "victim", "victim@example.com", "right-password", false); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		login(t, srv, "203.0.113.5:1000", "", "victim", "wrong") // an attacker hammering the account
	}
	if rec := login(t, srv, "198.51.100.20:2000", "", "victim", "right-password"); rec.Code != http.StatusOK {
		t.Fatalf("the real user from a different IP must still get in, got %d", rec.Code)
	}
}

func TestSuccessfulLoginResetsTheCounter(t *testing.T) {
	srv := newTestServer(t)
	if _, err := srv.DB.CreateUser(t.Context(), "u", "u@example.com", "pw", false); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		login(t, srv, "203.0.113.5:1", "", "u", "wrong")
	}
	if rec := login(t, srv, "203.0.113.5:1", "", "u", "pw"); rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	for i := 0; i < 4; i++ {
		if rec := login(t, srv, "203.0.113.5:1", "", "u", "wrong"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("after a success the counter starts over, attempt %d got %d", i+1, rec.Code)
		}
	}
}

func TestForwardedForIsHonouredOnlyFromATrustedProxy(t *testing.T) {
	srv := newTestServer(t)
	if _, err := srv.DB.CreateUser(t.Context(), "u", "u@example.com", "pw", false); err != nil {
		t.Fatal(err)
	}
	// Without trusted proxies, rotating X-Forwarded-For must NOT evade the limit.
	for i := 0; i < 5; i++ {
		login(t, srv, "203.0.113.5:1", "9.9.9."+strconv.Itoa(i), "u", "wrong")
	}
	if rec := login(t, srv, "203.0.113.5:1", "9.9.9.99", "u", "pw"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("a forged X-Forwarded-For must not reset the limit, got %d", rec.Code)
	}
}

type failingLimiter struct{}

func (failingLimiter) Blocked(context.Context, string, string) (bool, time.Duration, error) {
	return false, 0, errors.New("redis is down")
}
func (failingLimiter) RecordFailure(context.Context, string, string) error {
	return errors.New("redis is down")
}
func (failingLimiter) RecordSuccess(context.Context, string, string) error {
	return errors.New("redis is down")
}

func TestLoginFailsOpenWhenTheLimiterIsDown(t *testing.T) {
	srv := newTestServer(t)
	srv.LoginLimiter = failingLimiter{}
	if _, err := srv.DB.CreateUser(t.Context(), "u", "u@example.com", "pw", false); err != nil {
		t.Fatal(err)
	}
	if rec := login(t, srv, "203.0.113.5:1", "", "u", "pw"); rec.Code != http.StatusOK {
		t.Fatalf("a Redis outage must not take login down, got %d", rec.Code)
	}
	if rec := login(t, srv, "203.0.113.5:1", "", "u", "wrong"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a wrong password is still 401 with the limiter down, got %d", rec.Code)
	}
}
