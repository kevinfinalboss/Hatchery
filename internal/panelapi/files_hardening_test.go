package panelapi

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"sort"
	"testing"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

func zipBytes(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(content))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestStatusForSFTPDefaultsToBadGateway(t *testing.T) {
	if got := statusForSFTP(errors.New("connection lost")); got != http.StatusBadGateway {
		t.Errorf("generic transfer error: %d, want 502", got)
	}
	if got := statusForSFTP(os.ErrNotExist); got != http.StatusNotFound {
		t.Errorf("not exist: %d", got)
	}
	if got := statusForSFTP(os.ErrPermission); got != http.StatusForbidden {
		t.Errorf("permission: %d", got)
	}
}

func TestDecompressRejectsZipSlipAndWritesNothing(t *testing.T) {
	srv, gs, token := newFileManagerTestServer(t, gameserversv1alpha1.GameServerStateRunning)
	base := orgURL("/gameservers/" + gs.Name)
	evil := zipBytes(t, map[string]string{"ok.txt": "fine", "../../escaped.txt": "nope"})
	doRawRequest(t, srv, http.MethodPut, base+"/files/content?path=/evil.zip", token, evil)
	doRequest(t, srv, http.MethodPost, base+"/files/mkdir", token, mkdirRequest{Path: "/out"})

	rec := doRequest(t, srv, http.MethodPost, base+"/files/decompress", token, decompressRequest{Path: "/evil.zip", Dest: "/out"})
	if rec.Code != http.StatusUnprocessableEntity || !bytes.Contains(rec.Body.Bytes(), []byte("escaped.txt")) {
		t.Fatalf("zip-slip: %d %s", rec.Code, rec.Body)
	}
	if got := listDir(t, srv, gs, token, "/out"); len(got) != 0 {
		t.Fatalf("nothing may be extracted, got %v", got)
	}
}

func TestDecompressErrorPaths(t *testing.T) {
	srv, gs, token := newFileManagerTestServer(t, gameserversv1alpha1.GameServerStateRunning)
	base := orgURL("/gameservers/" + gs.Name)
	doRawRequest(t, srv, http.MethodPut, base+"/files/content?path=/notzip.zip", token, []byte("plain text"))

	if rec := doRequest(t, srv, http.MethodPost, base+"/files/decompress", token, decompressRequest{Path: "/notzip.zip", Dest: "/"}); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid zip: %d", rec.Code)
	}
	if rec := doRequest(t, srv, http.MethodPost, base+"/files/decompress", token, decompressRequest{Path: "/x.zip"}); rec.Code != http.StatusBadRequest {
		t.Errorf("missing dest: %d", rec.Code)
	}
	if rec := doRequest(t, srv, http.MethodPost, base+"/files/decompress", token, decompressRequest{Path: "/missing.zip", Dest: "/"}); rec.Code != http.StatusNotFound {
		t.Errorf("missing source: %d", rec.Code)
	}
}

func TestDownloadZipNamesDoNotCollide(t *testing.T) {
	srv, gs, token := newFileManagerTestServer(t, gameserversv1alpha1.GameServerStateRunning)
	base := orgURL("/gameservers/" + gs.Name)
	putFile(t, srv, gs, token, "/world/a/config.yml", []byte("A"))
	putFile(t, srv, gs, token, "/world/b/config.yml", []byte("B"))

	rec := doRequest(t, srv, http.MethodGet, base+"/files/download?paths=/world/a/config.yml&paths=/world/b/config.yml", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("download: %d %s", rec.Code, rec.Body)
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	contents := map[string]string{}
	for _, f := range zr.File {
		names = append(names, f.Name)
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		contents[f.Name] = string(b)
	}
	sort.Strings(names)
	if len(names) != 2 || names[0] != "a/config.yml" || names[1] != "b/config.yml" || contents["a/config.yml"] != "A" || contents["b/config.yml"] != "B" {
		t.Fatalf("zip entries = %v %v", names, contents)
	}
}

func TestDeleteFilesReportsPartialFailure(t *testing.T) {
	srv, gs, token := newFileManagerTestServer(t, gameserversv1alpha1.GameServerStateRunning)
	base := orgURL("/gameservers/" + gs.Name)
	putFile(t, srv, gs, token, "/a.txt", []byte("a"))
	putFile(t, srv, gs, token, "/b.txt", []byte("b"))

	rec := doRequest(t, srv, http.MethodPost, base+"/files/delete", token, deleteFilesRequest{Paths: []string{"/a.txt", "/missing.txt", "/b.txt"}})
	if rec.Code != http.StatusMultiStatus {
		t.Fatalf("partial delete: %d %s", rec.Code, rec.Body)
	}
	var body deleteFilesResult
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body.Deleted) != 2 || len(body.Failed) != 1 || body.Failed[0].Path != "/missing.txt" {
		t.Fatalf("result = %+v", body)
	}
	if got := listDir(t, srv, gs, token, "/"); len(got) != 0 {
		t.Fatalf("the existing files must be gone even though one failed: %v", got)
	}
	if rec := doRequest(t, srv, http.MethodPost, base+"/files/delete", token, deleteFilesRequest{Paths: []string{"/nothing"}}); rec.Code != http.StatusMultiStatus {
		t.Fatalf("all failed: %d", rec.Code)
	}
}
