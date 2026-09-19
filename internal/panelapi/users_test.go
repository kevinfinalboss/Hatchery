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
		{http.MethodPost, "/api/v1/users"},
	} {
		rec := doRequest(t, srv, req.method, req.path, nonAdmin, createUserRequest{})
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s as non-admin: expected 403, got %d", req.method, req.path, rec.Code)
		}
	}
}

func TestCreateListDeleteUser(t *testing.T) {
	srv := newTestServer(t)
	admin := adminToken(t, srv)

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/users", admin, createUserRequest{Username: "newbie", Password: "password"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created userResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.IsAdmin {
		t.Fatal("expected a non-admin user")
	}

	rec = doRequest(t, srv, http.MethodPost, "/api/v1/users", admin, createUserRequest{Username: "newbie", Password: "password"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate username: expected 409, got %d", rec.Code)
	}

	rec = doRequest(t, srv, http.MethodGet, "/api/v1/users", admin, nil)
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

func TestGrantAndRevokeGameServerPermission(t *testing.T) {
	srv := newTestServer(t)
	admin := adminToken(t, srv)

	target, err := srv.DB.CreateUser(t.Context(), "scoped-user", "password", false)
	if err != nil {
		t.Fatal(err)
	}

	grantPath := fmt.Sprintf("/api/v1/users/%d/permissions", target.ID)
	rec := doRequest(t, srv, http.MethodPost, grantPath, admin, grantPermissionRequest{Namespace: "default", Name: "some-server"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("grant: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, srv, http.MethodGet, grantPath, admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list permissions: expected 200, got %d", rec.Code)
	}
	var refs []paneldb.GameServerRef
	if err := json.Unmarshal(rec.Body.Bytes(), &refs); err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].Name != "some-server" {
		t.Fatalf("expected one grant for some-server, got %+v", refs)
	}

	revokePath := fmt.Sprintf("/api/v1/users/%d/permissions/default/some-server", target.ID)
	rec = doRequest(t, srv, http.MethodDelete, revokePath, admin, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("revoke: expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, srv, http.MethodGet, grantPath, admin, nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &refs); err != nil {
		t.Fatal(err)
	}
	if len(refs) != 0 {
		t.Fatalf("expected no grants after revoke, got %+v", refs)
	}
}
