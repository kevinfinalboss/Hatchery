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
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

// Every test in this package needs paneldb for auth, so — like paneldb's own
// tests — they need a real Postgres and skip without one. See
// internal/paneldb/store_test.go for how to point POSTGRES_TEST_DSN at one.
//
// This gets its own isolated database (not just a reset "public" schema) for
// the same reason internal/paneldb's own test helper does: `go test ./...`
// runs this package's tests and paneldb's tests concurrently, and both point
// at the same POSTGRES_TEST_DSN — a schema reset in one could wipe out
// tables the other's test just created mid-run. See the longer comment on
// paneldb's own newIsolatedTestDB.
func newTestStore(t *testing.T) *paneldb.Store {
	t.Helper()
	db := newIsolatedTestDB(t)
	if err := paneldb.Migrate(context.Background(), db); err != nil {
		t.Fatalf("running migrations: %v", err)
	}
	return paneldb.NewStore(db)
}

func newIsolatedTestDB(t *testing.T) *sql.DB {
	t.Helper()
	baseDSN := os.Getenv("POSTGRES_TEST_DSN")
	if baseDSN == "" {
		t.Skip("POSTGRES_TEST_DSN not set, skipping panelapi tests (they need paneldb for auth)")
	}

	ctx := context.Background()
	admin, err := paneldb.Open(ctx, baseDSN)
	if err != nil {
		t.Fatalf("opening admin connection: %v", err)
	}
	defer admin.Close()

	dbName := fmt.Sprintf("panelapi_test_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+dbName); err != nil {
		t.Fatalf("creating test database %s: %v", dbName, err)
	}
	t.Cleanup(func() {
		cleanup, err := paneldb.Open(context.Background(), baseDSN)
		if err != nil {
			return
		}
		defer cleanup.Close()
		_, _ = cleanup.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+dbName)
	})

	u, err := url.Parse(baseDSN)
	if err != nil {
		t.Fatalf("POSTGRES_TEST_DSN must be a postgres:// URL: %v", err)
	}
	u.Path = "/" + dbName

	db, err := paneldb.Open(ctx, u.String())
	if err != nil {
		t.Fatalf("opening test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newTestServer(t *testing.T, objs ...client.Object) *Server {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := gameserversv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).WithStatusSubresource(&gameserversv1alpha1.GameServer{}).Build()
	// Clientset/RESTConfig are nil: none of the handlers exercised in this
	// package's tests touch the log/attach subresources that need them.
	return NewServer(c, nil, nil, newTestStore(t), "example.com/sftp-agent:test")
}

// adminToken creates a fresh admin user and returns a live session token for
// them — the default identity for tests that aren't specifically about the
// permission boundary.
func adminToken(t *testing.T, srv *Server) string {
	t.Helper()
	return newUserToken(t, srv, "test-admin-"+t.Name(), true)
}

// newUserToken creates a user (admin or not) and returns a live session
// token for them.
func newUserToken(t *testing.T, srv *Server, username string, isAdmin bool) string {
	t.Helper()
	u, err := srv.DB.CreateUser(context.Background(), username, "password", isAdmin)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := srv.DB.CreateSession(context.Background(), u.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func doRequest(t *testing.T, srv *Server, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	return rec
}

func TestAuthRejectsMissingOrWrongToken(t *testing.T) {
	srv := newTestServer(t)
	token := adminToken(t, srv)

	if rec := doRequest(t, srv, http.MethodGet, "/api/v1/gameservers", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: expected 401, got %d", rec.Code)
	}
	if rec := doRequest(t, srv, http.MethodGet, "/api/v1/gameservers", "wrong-token", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: expected 401, got %d", rec.Code)
	}
	if rec := doRequest(t, srv, http.MethodGet, "/api/v1/gameservers", token, nil); rec.Code != http.StatusOK {
		t.Fatalf("correct token: expected 200, got %d", rec.Code)
	}
}

func TestHealthzDoesNotRequireAuth(t *testing.T) {
	srv := newTestServer(t)
	rec := doRequest(t, srv, http.MethodGet, "/healthz", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestEggListAndGet(t *testing.T) {
	egg := &gameserversv1alpha1.Egg{
		ObjectMeta: metav1.ObjectMeta{Name: "minecraft", Namespace: "default"},
		Spec:       gameserversv1alpha1.EggSpec{Image: "itzg/minecraft-server:latest", StartCommand: "/image/scripts/start"},
	}
	srv := newTestServer(t, egg)
	token := adminToken(t, srv)

	rec := doRequest(t, srv, http.MethodGet, "/api/v1/eggs?namespace=default", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list eggs: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var list gameserversv1alpha1.EggList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0].Name != "minecraft" {
		t.Fatalf("expected one egg named minecraft, got %+v", list.Items)
	}

	rec = doRequest(t, srv, http.MethodGet, "/api/v1/eggs/default/minecraft", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get egg: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, srv, http.MethodGet, "/api/v1/eggs/default/does-not-exist", token, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get missing egg: expected 404, got %d", rec.Code)
	}
}

func TestGameServerCRUDAndState(t *testing.T) {
	srv := newTestServer(t)
	token := adminToken(t, srv)

	createReq := createGameServerRequest{
		Name:      "my-server",
		Namespace: "default",
		Spec: gameserversv1alpha1.GameServerSpec{
			EggRef:  gameserversv1alpha1.GameServerEggRef{Name: "minecraft"},
			State:   gameserversv1alpha1.GameServerStateRunning,
			Storage: gameserversv1alpha1.GameServerStorage{Size: "1Gi"},
		},
	}
	rec := doRequest(t, srv, http.MethodPost, "/api/v1/gameservers", token, createReq)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, srv, http.MethodGet, "/api/v1/gameservers/default/my-server", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, srv, http.MethodGet, "/api/v1/gameservers?namespace=default", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", rec.Code)
	}
	var list gameserversv1alpha1.GameServerList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("expected one gameserver, got %d", len(list.Items))
	}

	rec = doRequest(t, srv, http.MethodPatch, "/api/v1/gameservers/default/my-server/state", token,
		setStateRequest{State: gameserversv1alpha1.GameServerStateStopped})
	if rec.Code != http.StatusOK {
		t.Fatalf("set state: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var updated gameserversv1alpha1.GameServer
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Spec.State != gameserversv1alpha1.GameServerStateStopped {
		t.Fatalf("expected state Stopped, got %q", updated.Spec.State)
	}

	rec = doRequest(t, srv, http.MethodPatch, "/api/v1/gameservers/default/my-server/state", token,
		setStateRequest{State: "Sideways"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid state: expected 400, got %d", rec.Code)
	}

	rec = doRequest(t, srv, http.MethodDelete, "/api/v1/gameservers/default/my-server", token, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("delete: expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, srv, http.MethodGet, "/api/v1/gameservers/default/my-server", token, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete: expected 404, got %d", rec.Code)
	}
}

func TestCreateGameServerRequiresNameAndNamespace(t *testing.T) {
	srv := newTestServer(t)
	token := adminToken(t, srv)
	rec := doRequest(t, srv, http.MethodPost, "/api/v1/gameservers", token, createGameServerRequest{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestNonAdminCannotCreateOrDeleteGameServers(t *testing.T) {
	srv := newTestServer(t)
	token := newUserToken(t, srv, "regular-user", false)

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/gameservers", token, createGameServerRequest{
		Name: "nope", Namespace: "default",
		Spec: gameserversv1alpha1.GameServerSpec{EggRef: gameserversv1alpha1.GameServerEggRef{Name: "minecraft"}, Storage: gameserversv1alpha1.GameServerStorage{Size: "1Gi"}},
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("create: expected 403, got %d", rec.Code)
	}

	rec = doRequest(t, srv, http.MethodDelete, "/api/v1/gameservers/default/whatever", token, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("delete: expected 403, got %d", rec.Code)
	}
}
