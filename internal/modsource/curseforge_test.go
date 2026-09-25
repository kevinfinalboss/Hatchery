package modsource

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func curseForgeServer(t *testing.T, routes map[string]string) (*CurseForge, map[string]*http.Request, map[string]string) {
	t.Helper()
	reqs := map[string]*http.Request{}
	bodies := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "test-key" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		reqs[r.URL.Path] = r
		b, _ := io.ReadAll(r.Body)
		bodies[r.URL.Path] = string(b)
		f, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(fixture(t, f))
	}))
	t.Cleanup(srv.Close)
	c := NewCurseForge("test-key", "hatchery-test/1")
	c.http.base = srv.URL
	return c, reqs, bodies
}

func TestCurseForgeSearch(t *testing.T) {
	c, reqs, _ := curseForgeServer(t, map[string]string{"/v1/mods/search": "curseforge/search.json"})
	page, err := c.Search(context.Background(), SearchQuery{Filter: Filter{Kind: KindMod, Loaders: []string{"fabric"}, GameVersion: "26.3"}, Query: "modmenu", Page: 2})
	if err != nil {
		t.Fatal(err)
	}
	q := reqs["/v1/mods/search"].URL.Query()
	if q.Get("gameId") != "432" || q.Get("classId") != "6" || q.Get("modLoaderType") != "4" || q.Get("gameVersion") != "26.3" ||
		q.Get("searchFilter") != "modmenu" || q.Get("index") != "40" || q.Get("pageSize") != "20" {
		t.Errorf("params: %v", q)
	}
	r := page.Results[0]
	if r.Source != "curseforge" || r.ProjectID != "308702" || r.Title != "Mod Menu" || r.Author != "Prospector" || r.DistributionBlocked ||
		r.PageURL != "https://www.curseforge.com/minecraft/mc-mods/modmenu" || page.Total != 10 {
		t.Errorf("result = %+v total %d", r, page.Total)
	}
	if !page.Results[1].DistributionBlocked {
		t.Error("allowModDistribution=false must be DistributionBlocked")
	}
}

func TestCurseForgePluginSearchUsesBukkitClassWithoutLoader(t *testing.T) {
	c, reqs, _ := curseForgeServer(t, map[string]string{"/v1/mods/search": "curseforge/search.json"})
	if _, err := c.Search(context.Background(), SearchQuery{Filter: Filter{Kind: KindPlugin, Loaders: []string{"paper"}}}); err != nil {
		t.Fatal(err)
	}
	q := reqs["/v1/mods/search"].URL.Query()
	if q.Get("classId") != "5" || q.Has("modLoaderType") {
		t.Errorf("params: %v", q)
	}
}

func TestCurseForgeVersions(t *testing.T) {
	c, _, _ := curseForgeServer(t, map[string]string{"/v1/mods/308702/files": "curseforge/files.json"})
	vs, err := c.Versions(context.Background(), "308702", Filter{Kind: KindMod, Loaders: []string{"fabric"}})
	if err != nil {
		t.Fatal(err)
	}
	v := vs[0]
	if v.ID != "7001" || v.File.SHA1 != strings.Repeat("a", 40) || v.File.URL == "" || v.File.Filename != "modmenu-18.0.0.jar" {
		t.Errorf("v = %+v", v)
	}
	if len(v.Dependencies) != 1 || v.Dependencies[0].ProjectID != "306612" {
		t.Errorf("only required deps: %+v", v.Dependencies)
	}
	if !vs[1].DistributionBlocked {
		t.Error("a file without downloadUrl is blocked")
	}
}

func TestCurseForgeIdentify(t *testing.T) {
	c, _, bodies := curseForgeServer(t, map[string]string{
		"/v1/fingerprints/432": "curseforge/fingerprints.json",
		"/v1/mods":             "curseforge/mods.json",
	})
	known := FileHashes{SHA1: "sha-known", Fingerprint: 222}
	got, err := c.Identify(context.Background(), []FileHashes{known, {SHA1: "sha-unknown", Fingerprint: 333}}, Filter{Kind: KindMod, Loaders: []string{"fabric"}, GameVersion: "26.3"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(bodies["/v1/fingerprints/432"], "[222,333]") {
		t.Errorf("body: %s", bodies["/v1/fingerprints/432"])
	}
	m, ok := got["sha-known"]
	if !ok || m.Project.Title != "Mod Menu" || m.Version.ID != "6001" || m.Latest == nil || m.Latest.ID != "7001" {
		t.Fatalf("match = %+v", m)
	}
	if _, ok := got["sha-unknown"]; ok {
		t.Error("unmatched fingerprint must not be in the map")
	}
	// On 26.2 the newest compatible file is the installed one: no update.
	got, _ = c.Identify(context.Background(), []FileHashes{known}, Filter{Kind: KindMod, Loaders: []string{"fabric"}, GameVersion: "26.2"})
	if got["sha-known"].Latest != nil {
		t.Errorf("26.2: unexpected update %+v", got["sha-known"].Latest)
	}
}
