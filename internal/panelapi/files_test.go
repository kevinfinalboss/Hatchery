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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

// doRawRequest is like doRequest but sends body verbatim instead of
// JSON-marshaling it — needed for PUT .../files/content, whose body is raw
// file bytes, not a JSON envelope.
func doRawRequest(t *testing.T, srv *Server, method, path, token string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	return rec
}

func TestFilesListMkdirWriteReadRenameDelete(t *testing.T) {
	srv, gs, token := newFileManagerTestServer(t, gameserversv1alpha1.GameServerStateRunning)
	base := "/api/v1/gameservers/default/" + gs.Name

	rec := doRequest(t, srv, http.MethodPost, base+"/files/mkdir", token, mkdirRequest{Path: "/configs"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("mkdir: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doRawRequest(t, srv, http.MethodPut, base+"/files/content?path=/configs/a.txt", token, []byte("hello"))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("write: expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, srv, http.MethodGet, base+"/files/content?path=/configs/a.txt", token, nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "hello" {
		t.Fatalf("read: expected 200/hello, got %d/%s", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, srv, http.MethodGet, base+"/files?path=/configs", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var entries []fileEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "a.txt" || entries[0].IsDir {
		t.Fatalf("expected one file entry a.txt, got %+v", entries)
	}

	rec = doRequest(t, srv, http.MethodPost, base+"/files/rename", token, renameRequest{From: "/configs/a.txt", To: "/configs/b.txt"})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("rename: expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = doRequest(t, srv, http.MethodGet, base+"/files/content?path=/configs/b.txt", token, nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "hello" {
		t.Fatalf("read renamed file: expected 200/hello, got %d/%s", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, srv, http.MethodPost, base+"/files/delete", token, deleteFilesRequest{Paths: []string{"/configs"}})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = doRequest(t, srv, http.MethodGet, base+"/files?path=/", token, nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty root after recursive delete, got %+v", entries)
	}
}

func TestFilesRequireGameServerAccess(t *testing.T) {
	gs, secret := newTestGameServerWithSecret("gs-files-scoped", gameserversv1alpha1.GameServerStateRunning)
	srv := newTestServer(t, gs, secret)
	withoutGrant := newUserToken(t, srv, "no-grant-user-files", false)

	rec := doRequest(t, srv, http.MethodGet, "/api/v1/gameservers/default/gs-files-scoped/files?path=/", withoutGrant, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without a grant, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFilesCopyFileAndDirectory(t *testing.T) {
	srv, gs, token := newFileManagerTestServer(t, gameserversv1alpha1.GameServerStateRunning)
	base := "/api/v1/gameservers/default/" + gs.Name

	doRequest(t, srv, http.MethodPost, base+"/files/mkdir", token, mkdirRequest{Path: "/src"})
	doRawRequest(t, srv, http.MethodPut, base+"/files/content?path=/src/a.txt", token, []byte("hi"))

	rec := doRequest(t, srv, http.MethodPost, base+"/files/copy", token, copyRequest{From: "/src", To: "/dst"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("copy dir: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, srv, http.MethodGet, base+"/files/content?path=/dst/a.txt", token, nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "hi" {
		t.Fatalf("copied file: expected 200/hi, got %d/%s", rec.Code, rec.Body.String())
	}
	// original must still exist — this is a copy, not a move.
	rec = doRequest(t, srv, http.MethodGet, base+"/files/content?path=/src/a.txt", token, nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "hi" {
		t.Fatalf("original file: expected 200/hi, got %d/%s", rec.Code, rec.Body.String())
	}
}
