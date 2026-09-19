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
	"encoding/json"
	"net/http"
	"testing"
)

func TestLoginSuccessAndFailure(t *testing.T) {
	srv := newTestServer(t)
	if _, err := srv.DB.CreateUser(t.Context(), "alice", "correct-password", false); err != nil {
		t.Fatal(err)
	}

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/auth/login", "", loginRequest{Username: "alice", Password: "wrong"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: expected 401, got %d", rec.Code)
	}

	rec = doRequest(t, srv, http.MethodPost, "/api/v1/auth/login", "", loginRequest{Username: "nobody", Password: "whatever"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown user: expected 401, got %d", rec.Code)
	}

	rec = doRequest(t, srv, http.MethodPost, "/api/v1/auth/login", "", loginRequest{Username: "alice", Password: "correct-password"})
	if rec.Code != http.StatusOK {
		t.Fatalf("correct credentials: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp loginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Token == "" {
		t.Fatal("expected a non-empty session token")
	}
	if resp.User.Username != "alice" || resp.User.IsAdmin {
		t.Fatalf("unexpected user in response: %+v", resp.User)
	}

	// The token from login should work against an authenticated route.
	rec = doRequest(t, srv, http.MethodGet, "/api/v1/auth/me", resp.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("/me with fresh token: expected 200, got %d", rec.Code)
	}
}

func TestLogoutRevokesSession(t *testing.T) {
	srv := newTestServer(t)
	token := adminToken(t, srv)

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/auth/logout", token, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout: expected 204, got %d", rec.Code)
	}

	rec = doRequest(t, srv, http.MethodGet, "/api/v1/auth/me", token, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("using a revoked token: expected 401, got %d", rec.Code)
	}
}

func TestMeReturnsCurrentUser(t *testing.T) {
	srv := newTestServer(t)
	token := newUserToken(t, srv, "bob", false)

	rec := doRequest(t, srv, http.MethodGet, "/api/v1/auth/me", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var u userResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &u); err != nil {
		t.Fatal(err)
	}
	if u.Username != "bob" || u.IsAdmin {
		t.Fatalf("unexpected user: %+v", u)
	}
}
