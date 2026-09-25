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
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

func TestUserManagementIsAdminOnly(t *testing.T) {
	srv := newTestServer(t)
	nonAdmin := newUserToken(t, srv, "regular", false)

	for _, req := range []struct {
		method, path string
	}{
		{http.MethodGet, "/api/v1/users"},
		{http.MethodPatch, "/api/v1/users/1"},
	} {
		rec := doRequest(t, srv, req.method, req.path, nonAdmin, nil)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s as non-admin: expected 403, got %d", req.method, req.path, rec.Code)
		}
	}
}

func TestListDeleteUser(t *testing.T) {
	srv := newTestServer(t)
	admin := adminToken(t, srv)

	created, err := srv.DB.CreateUser(t.Context(), "newbie", "newbie@example.com", "password", false)
	if err != nil {
		t.Fatal(err)
	}

	rec := doRequest(t, srv, http.MethodGet, "/api/v1/users", admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", rec.Code)
	}
	var list []userResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, u := range list {
		found = found || u.Username == "newbie"
	}
	if !found {
		t.Fatalf("expected newbie in user list, got %+v", list)
	}

	rec = doRequest(t, srv, http.MethodDelete, fmt.Sprintf("/api/v1/users/%d", created.ID), admin, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = doRequest(t, srv, http.MethodDelete, fmt.Sprintf("/api/v1/users/%d", created.ID), admin, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete again: expected 404, got %d", rec.Code)
	}
}

func TestSetPlatformAdmin(t *testing.T) {
	srv := newTestServer(t)
	adminTok := newUserToken(t, srv, "boss", true)
	newUserToken(t, srv, "bob", false)
	bob := userID(t, srv, "bob")
	boss := userID(t, srv, "boss")

	patch := func(id int64, isAdmin bool) int {
		return doRequest(t, srv, http.MethodPatch, fmt.Sprintf("/api/v1/users/%d", id), adminTok, map[string]bool{"isAdmin": isAdmin}).Code
	}
	if got := patch(boss, false); got != http.StatusConflict {
		t.Errorf("demoting yourself: %d", got)
	}
	if got := patch(bob, true); got != http.StatusOK {
		t.Fatalf("promoting bob: %d", got)
	}
	u, _ := srv.DB.GetUser(t.Context(), bob)
	if !u.IsAdmin {
		t.Fatal("bob not promoted")
	}
	if got := patch(999999, true); got != http.StatusNotFound {
		t.Errorf("unknown user: %d", got)
	}
	if got := doRequest(t, srv, http.MethodPost, "/api/v1/users", adminTok, map[string]any{"username": "x", "password": "password1"}).Code; got != http.StatusMethodNotAllowed && got != http.StatusNotFound {
		t.Errorf("POST /users still exists: %d", got)
	}
}

func TestLastPlatformAdminCannotBeDemotedByAnother(t *testing.T) {
	srv := newTestServer(t)
	// Two admins; one demotes the other, then the survivor cannot be demoted
	// through the store (the handler blocks self-demotion earlier).
	a := newUserToken(t, srv, "a", true)
	newUserToken(t, srv, "b", true)
	if got := doRequest(t, srv, http.MethodPatch, fmt.Sprintf("/api/v1/users/%d", userID(t, srv, "b")), a, map[string]bool{"isAdmin": false}).Code; got != http.StatusOK {
		t.Fatalf("demoting b: %d", got)
	}
	if err := srv.DB.SetAdmin(t.Context(), userID(t, srv, "a"), false); !errors.Is(err, paneldb.ErrLastAdmin) {
		t.Fatalf("store let the last admin go: %v", err)
	}
}

func TestDeleteUserIsRefusedForTheSoleOwnerOfAnOrg(t *testing.T) {
	srv := newTestServer(t)
	admin := adminToken(t, srv)

	rec := doRequest(t, srv, http.MethodDelete, fmt.Sprintf("/api/v1/users/%d", userID(t, srv, "fixture-owner")), admin, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for the sole owner of an org, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteUserIsRefusedForTheInitialAdmin(t *testing.T) {
	srv := newTestServer(t)
	admin := adminToken(t, srv)
	initial, err := srv.DB.CreateUser(t.Context(), BootstrapAdminUsername, "admin@example.com", "password", true)
	if err != nil {
		t.Fatal(err)
	}

	rec := doRequest(t, srv, http.MethodDelete, fmt.Sprintf("/api/v1/users/%d", initial.ID), admin, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for the initial admin, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := srv.DB.GetUser(t.Context(), initial.ID); err != nil {
		t.Fatalf("initial admin should still exist: %v", err)
	}
}
