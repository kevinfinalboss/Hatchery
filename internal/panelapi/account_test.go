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

package panelapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kevinfinalboss/Hatchery/internal/mailer"
)

// tokenFromLink extracts the token of the first "#token=" link in text.
func tokenFromLink(t *testing.T, text string) string {
	t.Helper()
	i := strings.Index(text, "#token=")
	if i < 0 {
		t.Fatalf("no link in %q", text)
	}
	rest := text[i+len("#token="):]
	if j := strings.IndexAny(rest, "\r\n \"<"); j >= 0 {
		rest = rest[:j]
	}
	tok, err := url.QueryUnescape(rest)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// newTestServerWithMail is newTestServer with a recording mailer, a public
// URL and synchronous background work.
func newTestServerWithMail(t *testing.T) (*Server, *mailer.Recorder) {
	t.Helper()
	srv := newTestServer(t)
	rec := &mailer.Recorder{}
	srv.Mailer = rec
	srv.PublicURL = "https://panel.example.com"
	srv.background = func(f func()) { f() }
	return srv, rec
}

// sessionFor creates a user with a known password and returns a session token.
func sessionFor(t *testing.T, srv *Server, username string) string {
	t.Helper()
	u, err := srv.DB.CreateUser(context.Background(), username, username+"@example.com", "password1", false)
	if err != nil {
		t.Fatal(err)
	}
	tok, _, err := srv.DB.CreateSession(context.Background(), u.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestFeatures(t *testing.T) {
	srv := newTestServer(t)
	rec := doRequest(t, srv, http.MethodGet, "/api/v1/auth/features", "", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "{\"passwordReset\":false}\n" {
		t.Fatalf("without mail: %d %s", rec.Code, rec.Body)
	}
	srv, _ = newTestServerWithMail(t)
	rec = doRequest(t, srv, http.MethodGet, "/api/v1/auth/features", "", nil)
	if rec.Body.String() != "{\"passwordReset\":true}\n" {
		t.Fatalf("with mail: %s", rec.Body)
	}
}

func TestLoginWithEmailAnyCase(t *testing.T) {
	srv := newTestServer(t)
	sessionFor(t, srv, "kevin")
	rec := doRequest(t, srv, http.MethodPost, "/api/v1/auth/login", "", map[string]string{"username": "KEVIN@example.com", "password": "password1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("login by e-mail: %d %s", rec.Code, rec.Body)
	}
}

func TestLoginLimiterSharedBetweenUsernameAndEmail(t *testing.T) {
	srv := newTestServer(t)
	sessionFor(t, srv, "kevin")
	for i := 0; i < 3; i++ {
		doRequest(t, srv, http.MethodPost, "/api/v1/auth/login", "", map[string]string{"username": "kevin", "password": "wrong"})
		doRequest(t, srv, http.MethodPost, "/api/v1/auth/login", "", map[string]string{"username": "kevin@example.com", "password": "wrong"})
	}
	// 6 failures under one key; the per-user+IP limit is 5.
	rec := doRequest(t, srv, http.MethodPost, "/api/v1/auth/login", "", map[string]string{"username": "kevin", "password": "password1"})
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("got %d, want 429 — alternating login forms must share the limit", rec.Code)
	}
}

func TestUpdateProfile(t *testing.T) {
	srv := newTestServer(t)
	tok := sessionFor(t, srv, "kevin")
	rec := doRequest(t, srv, http.MethodPatch, "/api/v1/me", tok, map[string]string{"displayName": "Kevin Gomes", "discord": "kevin.g"})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body)
	}
	var me userResponse
	_ = json.Unmarshal(doRequest(t, srv, http.MethodGet, "/api/v1/auth/me", tok, nil).Body.Bytes(), &me)
	if me.DisplayName != "Kevin Gomes" || me.Discord != "kevin.g" || me.Email != "kevin@example.com" {
		t.Fatalf("me = %+v", me)
	}
	// Absent fields do not change; "" clears.
	doRequest(t, srv, http.MethodPatch, "/api/v1/me", tok, map[string]string{"discord": ""})
	_ = json.Unmarshal(doRequest(t, srv, http.MethodGet, "/api/v1/auth/me", tok, nil).Body.Bytes(), &me)
	if me.DisplayName != "Kevin Gomes" || me.Discord != "" {
		t.Fatalf("after clearing discord: %+v", me)
	}
	if rec := doRequest(t, srv, http.MethodPatch, "/api/v1/me", tok, map[string]string{"steamId": "123"}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid steam id: %d", rec.Code)
	}
}

func TestChangePasswordKeepsCurrentSessionOnly(t *testing.T) {
	srv := newTestServer(t)
	tok := sessionFor(t, srv, "kevin")
	u, _ := srv.DB.GetUserByUsername(context.Background(), "kevin")
	other, _, _ := srv.DB.CreateSession(context.Background(), u.ID, time.Hour)

	if rec := doRequest(t, srv, http.MethodPost, "/api/v1/me/password", tok, map[string]string{"currentPassword": "wrong", "newPassword": "password2"}); rec.Code != http.StatusForbidden {
		t.Fatalf("wrong current: %d", rec.Code)
	}
	if rec := doRequest(t, srv, http.MethodPost, "/api/v1/me/password", tok, map[string]string{"currentPassword": "password1", "newPassword": "short"}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("short new: %d", rec.Code)
	}
	if rec := doRequest(t, srv, http.MethodPost, "/api/v1/me/password", tok, map[string]string{"currentPassword": "password1", "newPassword": "password2"}); rec.Code != http.StatusNoContent {
		t.Fatalf("change: %d %s", rec.Code, rec.Body)
	}
	if rec := doRequest(t, srv, http.MethodGet, "/api/v1/auth/me", tok, nil); rec.Code != http.StatusOK {
		t.Fatalf("current session dropped: %d", rec.Code)
	}
	if rec := doRequest(t, srv, http.MethodGet, "/api/v1/auth/me", other, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("other session survived: %d", rec.Code)
	}
}

func TestForgotPasswordSameAnswerAndOnlyMailsRealAccounts(t *testing.T) {
	srv, mails := newTestServerWithMail(t)
	sessionFor(t, srv, "kevin")
	a := doRequest(t, srv, http.MethodPost, "/api/v1/auth/password/forgot", "", map[string]string{"email": "Kevin@example.com"})
	b := doRequest(t, srv, http.MethodPost, "/api/v1/auth/password/forgot", "", map[string]string{"email": "nobody@example.com"})
	if a.Code != http.StatusAccepted || b.Code != http.StatusAccepted || a.Body.String() != b.Body.String() {
		t.Fatalf("answers differ: %d %q vs %d %q", a.Code, a.Body, b.Code, b.Body)
	}
	sent := mails.Sent()
	if len(sent) != 1 || sent[0].To != "kevin@example.com" {
		t.Fatalf("sent = %+v", sent)
	}
	if !strings.Contains(sent[0].Text, "https://panel.example.com/reset-password#token=") {
		t.Fatalf("link does not use PublicURL: %s", sent[0].Text)
	}
}

func TestForgotPasswordIgnoresForgedHost(t *testing.T) {
	srv, mails := newTestServerWithMail(t)
	sessionFor(t, srv, "kevin")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/forgot", strings.NewReader(`{"email":"kevin@example.com"}`))
	req.Host = "evil.example.net"
	req.Header.Set("X-Forwarded-Host", "evil.example.net")
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if strings.Contains(mails.Sent()[0].Text, "evil") {
		t.Fatal("reset link built from the request host")
	}
}

func TestForgotPasswordRateLimitedPerEmail(t *testing.T) {
	srv, _ := newTestServerWithMail(t)
	for i := 0; i < 5; i++ {
		doRequest(t, srv, http.MethodPost, "/api/v1/auth/password/forgot", "", map[string]string{"email": "nobody@example.com"})
	}
	rec := doRequest(t, srv, http.MethodPost, "/api/v1/auth/password/forgot", "", map[string]string{"email": "nobody@example.com"})
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("6th request: %d", rec.Code)
	}
}

func TestForgotPasswordWithoutMail(t *testing.T) {
	srv := newTestServer(t)
	if rec := doRequest(t, srv, http.MethodPost, "/api/v1/auth/password/forgot", "", map[string]string{"email": "a@example.com"}); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", rec.Code)
	}
}

func TestChangeEmailWithoutMailAppliesAtOnce(t *testing.T) {
	srv := newTestServer(t)
	tok := sessionFor(t, srv, "kevin")
	rec := doRequest(t, srv, http.MethodPost, "/api/v1/me/email", tok, map[string]string{"newEmail": "Me@Example.com", "currentPassword": "password1"})
	var me userResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	if rec.Code != http.StatusOK || me.Email != "me@example.com" {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if rec := doRequest(t, srv, http.MethodPost, "/api/v1/me/email", tok, map[string]string{"newEmail": "x@example.com", "currentPassword": "wrong"}); rec.Code != http.StatusForbidden {
		t.Fatalf("wrong password: %d", rec.Code)
	}
}

func TestResetPasswordFlow(t *testing.T) {
	srv, mails := newTestServerWithMail(t)
	old := sessionFor(t, srv, "kevin")
	doRequest(t, srv, http.MethodPost, "/api/v1/auth/password/forgot", "", map[string]string{"email": "kevin@example.com"})
	tok := tokenFromLink(t, mails.Sent()[0].Text)

	if rec := doRequest(t, srv, http.MethodPost, "/api/v1/auth/password/reset", "", map[string]string{"token": tok, "password": "short"}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("short password: %d", rec.Code)
	}
	if rec := doRequest(t, srv, http.MethodPost, "/api/v1/auth/password/reset", "", map[string]string{"token": tok, "password": "password9"}); rec.Code != http.StatusNoContent {
		t.Fatalf("reset: %d %s", rec.Code, rec.Body)
	}
	if rec := doRequest(t, srv, http.MethodGet, "/api/v1/auth/me", old, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("old session survived reset: %d", rec.Code)
	}
	if rec := doRequest(t, srv, http.MethodPost, "/api/v1/auth/password/reset", "", map[string]string{"token": tok, "password": "password8"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("reused link: %d", rec.Code)
	}
}

func TestChangeEmailFlow(t *testing.T) {
	srv, mails := newTestServerWithMail(t)
	tok := sessionFor(t, srv, "kevin")
	sessionFor(t, srv, "taken")

	if rec := doRequest(t, srv, http.MethodPost, "/api/v1/me/email", tok, map[string]string{"newEmail": "taken@example.com", "currentPassword": "password1"}); rec.Code != http.StatusConflict {
		t.Fatalf("taken e-mail: %d", rec.Code)
	}
	if rec := doRequest(t, srv, http.MethodPost, "/api/v1/me/email", tok, map[string]string{"newEmail": "new@example.com", "currentPassword": "wrong"}); rec.Code != http.StatusForbidden {
		t.Fatalf("wrong password: %d", rec.Code)
	}
	if rec := doRequest(t, srv, http.MethodPost, "/api/v1/me/email", tok, map[string]string{"newEmail": "New@example.com", "currentPassword": "password1"}); rec.Code != http.StatusAccepted {
		t.Fatalf("request: %d %s", rec.Code, rec.Body)
	}
	sent := mails.Sent()
	if len(sent) != 2 || sent[0].To != "new@example.com" || sent[1].To != "kevin@example.com" {
		t.Fatalf("sent = %+v (want confirmation to new, notice to old)", sent)
	}
	confirm := tokenFromLink(t, sent[0].Text)
	if rec := doRequest(t, srv, http.MethodPost, "/api/v1/auth/email/confirm", "", map[string]string{"token": confirm}); rec.Code != http.StatusOK {
		t.Fatalf("confirm: %d %s", rec.Code, rec.Body)
	}
	var me userResponse
	_ = json.Unmarshal(doRequest(t, srv, http.MethodGet, "/api/v1/auth/me", tok, nil).Body.Bytes(), &me)
	if me.Email != "new@example.com" {
		t.Fatalf("email = %q", me.Email)
	}
}
