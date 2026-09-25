package modsource

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// modrinthServer serves fixtures by path and records the last request of each path.
func modrinthServer(t *testing.T, routes map[string]string) (*Modrinth, map[string]*http.Request, map[string][]byte) {
	t.Helper()
	reqs := map[string]*http.Request{}
	bodies := map[string][]byte{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ua := r.Header.Get("User-Agent"); ua != "hatchery-test/1" {
			t.Errorf("User-Agent = %q", ua)
		}
		reqs[r.URL.Path] = r
		if r.Body != nil {
			b := make([]byte, 1<<16)
			n, _ := r.Body.Read(b)
			bodies[r.URL.Path] = b[:n]
		}
		f, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture(t, f))
	}))
	t.Cleanup(srv.Close)
	m := NewModrinth("hatchery-test/1")
	m.http.base = srv.URL
	return m, reqs, bodies
}

func TestModrinthSearch(t *testing.T) {
	m, reqs, _ := modrinthServer(t, map[string]string{
		"/search":                   "modrinth/search.json",
		"/versions":                 "modrinth/versions_lithium.json",
		"/project/GLVp5tfO/version": "modrinth/empty_list.json",
		"/project/JNRr4jji/version": "modrinth/empty_list.json",
	})
	page, err := m.Search(context.Background(), SearchQuery{Filter: Filter{Kind: KindMod, Loaders: []string{"fabric"}, GameVersion: "26.3"}, Query: "lithium", Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	q := reqs["/search"].URL.Query()
	if q.Get("query") != "lithium" || q.Get("offset") != "20" || q.Get("limit") != "20" {
		t.Errorf("query params: %v", q)
	}
	var facets [][]string
	_ = json.Unmarshal([]byte(q.Get("facets")), &facets)
	if len(facets) != 3 || facets[0][0] != "project_type:mod" || facets[1][0] != "categories:fabric" || facets[2][0] != "versions:26.3" {
		t.Errorf("facets = %v", facets)
	}
	r := page.Results[0]
	if r.Source != "modrinth" || r.ProjectID != "gvQqBUqZ" || r.Title != "Lithium" || r.PageURL != "https://modrinth.com/mod/lithium" || page.Total == 0 {
		t.Errorf("result = %+v", r)
	}
}

func TestModrinthVersionsAndDependencies(t *testing.T) {
	m, reqs, _ := modrinthServer(t, map[string]string{"/project/modmenu/version": "modrinth/versions_modmenu.json"})
	vs, err := m.Versions(context.Background(), "modmenu", Filter{Kind: KindMod, Loaders: []string{"fabric"}, GameVersion: "26.3"})
	if err != nil {
		t.Fatal(err)
	}
	q := reqs["/project/modmenu/version"].URL.Query()
	if q.Get("loaders") != `["fabric"]` || q.Get("game_versions") != `["26.3"]` {
		t.Errorf("params: %v", q)
	}
	v := vs[0]
	if v.File.SHA1 == "" || v.File.SHA512 == "" || !strings.HasSuffix(v.File.Filename, ".jar") || v.File.URL == "" {
		t.Errorf("file = %+v", v.File)
	}
	if len(v.Dependencies) != 2 || v.Dependencies[1].ProjectID != "P7dR8mSH" {
		t.Errorf("deps = %+v", v.Dependencies)
	}
}

func TestModrinthIdentify(t *testing.T) {
	m, _, bodies := modrinthServer(t, map[string]string{
		"/version_files":        "modrinth/version_files.json",
		"/version_files/update": "modrinth/version_files_update.json",
		"/projects":             "modrinth/projects.json",
	})
	newest := FileHashes{SHA1: "91e91c955650afaf98e4acebee060a58ac99ade5"}
	older := FileHashes{SHA1: "435ad0289209bb72195e7a423756a03f5dfa5a9a"}
	got, err := m.Identify(context.Background(), []FileHashes{newest, older, {SHA1: "0000"}}, Filter{Kind: KindMod, Loaders: []string{"fabric"}, GameVersion: "26.3"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(bodies["/version_files/update"]), `"game_versions":["26.3"]`) {
		t.Errorf("update body: %s", bodies["/version_files/update"])
	}
	// For 26.3 the newest compatible version is WXHRsMRl: the older file (26.2 build) has an
	// update, the newer one does not.
	o := got[older.SHA1]
	if o.Project.Title != "Lithium" || o.Version.ID != "f7vZ0VWU" || o.Latest == nil || o.Latest.ID != "WXHRsMRl" {
		t.Errorf("older = %+v", o)
	}
	if n := got[newest.SHA1]; n.Version.ID != "WXHRsMRl" || n.Latest != nil {
		t.Errorf("newest = %+v", n)
	}
	if _, ok := got["0000"]; ok {
		t.Error("unknown hash must not be in the map")
	}
}

func TestModrinthErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search" {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	m := NewModrinth("hatchery-test/1")
	m.http.base = srv.URL
	_, err := m.Search(context.Background(), SearchQuery{Filter: Filter{Kind: KindMod}})
	var rl *RateLimitedError
	if !errors.As(err, &rl) || rl.RetryAfter.Seconds() != 7 {
		t.Errorf("429: err = %v", err)
	}
	if _, err := m.Versions(context.Background(), "x", Filter{}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("502: err = %v", err)
	}
}

func TestIsNewer(t *testing.T) {
	old := Version{ID: "a", PublishedAt: mustTime("2026-01-01T00:00:00Z")}
	newer := Version{ID: "b", PublishedAt: mustTime("2026-02-01T00:00:00Z")}
	if !IsNewer(newer, old) || IsNewer(old, newer) || IsNewer(old, old) {
		t.Fatal("IsNewer: a later, different version is an update; an older one or the same is not")
	}
}

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// With a game version and loaders, search keeps only projects that have one version matching
// both: the newest version is checked in one batch, and only projects whose newest version does
// not match get a per-project lookup (then cached).
func TestModrinthSearchKeepsOnlyCompatibleProjects(t *testing.T) {
	calls := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls[r.URL.Path]++
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search":
			_, _ = w.Write([]byte(`{"total_hits":3,"hits":[
				{"project_id":"a","slug":"a","title":"A","project_type":"mod","latest_version":"va"},
				{"project_id":"b","slug":"b","title":"B","project_type":"mod","latest_version":"vb"},
				{"project_id":"c","slug":"c","title":"C","project_type":"mod","latest_version":"vc"}]}`))
		case "/versions":
			_, _ = w.Write([]byte(`[
				{"id":"va","project_id":"a","loaders":["fabric"],"game_versions":["26.3"]},
				{"id":"vb","project_id":"b","loaders":["forge"],"game_versions":["26.3"]},
				{"id":"vc","project_id":"c","loaders":["fabric"],"game_versions":["26.4"]}]`))
		case "/project/b/version":
			_, _ = w.Write([]byte(`[]`))
		case "/project/c/version":
			_, _ = w.Write([]byte(`[{"id":"vc-old","project_id":"c","loaders":["fabric"],"game_versions":["26.3"]}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	m := NewModrinth("hatchery-test/1")
	m.http.base = srv.URL
	q := SearchQuery{Filter: Filter{Kind: KindMod, Loaders: []string{"fabric"}, GameVersion: "26.3"}, Query: "x"}

	page, err := m.Search(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range page.Results {
		ids = append(ids, r.ProjectID)
	}
	if strings.Join(ids, ",") != "a,c" {
		t.Fatalf("results = %v, want a,c (b has no fabric build for 26.3)", ids)
	}
	if calls["/versions"] != 1 || calls["/project/a/version"] != 0 {
		t.Errorf("calls = %v: newest versions in one batch, per-project only when needed", calls)
	}
	// A second identical search reuses the cached verdicts for b and c.
	if _, err := m.Search(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	if calls["/project/b/version"] != 1 || calls["/project/c/version"] != 1 {
		t.Errorf("verdicts were not cached: %v", calls)
	}
}

func TestModrinthSearchModpacks(t *testing.T) {
	m, reqs, _ := modrinthServer(t, map[string]string{"/search": "modrinth/search.json"})
	if _, err := m.Search(context.Background(), SearchQuery{Filter: Filter{Kind: KindModpack}, Query: "cobblemon"}); err != nil {
		t.Fatal(err)
	}
	var facets [][]string
	_ = json.Unmarshal([]byte(reqs["/search"].URL.Query().Get("facets")), &facets)
	if len(facets) != 1 || facets[0][0] != "project_type:modpack" {
		t.Fatalf("facets = %v", facets)
	}
}
