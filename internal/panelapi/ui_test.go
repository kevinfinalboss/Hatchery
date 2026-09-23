package panelapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUIHandler(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"index.html": "<html>app</html>", "assets/app-1a2b.js": "console.log(1)", "favicon.svg": "<svg/>"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	h := (&Server{UIDir: dir}).Routes()

	cases := []struct {
		path, wantBody, wantCache string
		wantStatus                int
	}{
		{"/", "<html>app</html>", "no-cache", http.StatusOK},
		{"/servers/acme/mc", "<html>app</html>", "no-cache", http.StatusOK},
		{"/assets/app-1a2b.js", "console.log(1)", "immutable", http.StatusOK},
		{"/favicon.svg", "<svg/>", "", http.StatusOK},
		{"/assets", "<html>app</html>", "no-cache", http.StatusOK}, // a directory is not a file: SPA route
		{"/api/v1/does-not-exist", "", "", http.StatusNotFound},
		{"/healthz", "", "", http.StatusOK},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != c.wantStatus {
			t.Errorf("%s: status %d, want %d", c.path, rec.Code, c.wantStatus)
		}
		if c.wantBody != "" && rec.Body.String() != c.wantBody {
			t.Errorf("%s: body %q, want %q", c.path, rec.Body.String(), c.wantBody)
		}
		if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, c.wantCache) {
			t.Errorf("%s: Cache-Control %q, want it to contain %q", c.path, got, c.wantCache)
		}
	}
}

func TestNoUIWithoutDir(t *testing.T) {
	rec := httptest.NewRecorder()
	(&Server{}).Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404 when no UI directory is configured", rec.Code)
	}
}
