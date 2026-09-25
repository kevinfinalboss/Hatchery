package panelapi

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/modsource"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

func sha1Hex(b []byte) string {
	s := sha1.Sum(b)
	return hex.EncodeToString(s[:])
}

// jarHost serves each file's content at /<name>.
func jarHost(t *testing.T, files map[string][]byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := files[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func modVer(host *httptest.Server, project, id, filename string, content []byte, published time.Time, deps ...string) modsource.Version {
	v := modsource.Version{
		Source: "modrinth", ID: id, ProjectID: project, VersionNumber: id, PublishedAt: published,
		File: modsource.File{URL: host.URL + "/" + id, Filename: filename, Size: int64(len(content)), SHA1: sha1Hex(content)},
	}
	for _, d := range deps {
		v.Dependencies = append(v.Dependencies, modsource.Dependency{ProjectID: d})
	}
	return v
}

func listDir(t *testing.T, srv *Server, gs *gameserversv1alpha1.GameServer, token, dir string) []string {
	t.Helper()
	rec := doRequest(t, srv, http.MethodGet, orgURL("/gameservers/"+gs.Name+"/files?path="+dir), token, nil)
	if rec.Code != http.StatusOK {
		return nil
	}
	var entries []fileEntry
	_ = json.Unmarshal(rec.Body.Bytes(), &entries)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
	}
	sort.Strings(names)
	return names
}

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func TestInstallModWithDependency(t *testing.T) {
	srv, gs, token, src := newModsTestServer(t, "1.21.4")
	a, b := []byte("plugin-a"), []byte("lib-b")
	host := jarHost(t, map[string][]byte{"a1": a, "b1": b})
	src.versions["a"] = []modsource.Version{modVer(host, "a", "a1", "a-1.0.jar", a, t0, "b")}
	src.versions["b"] = []modsource.Version{modVer(host, "b", "b1", "b-1.0.jar", b, t0)}

	rec := doRequest(t, srv, http.MethodPost, modsURL(gs, "/install"), token, map[string]string{"source": "modrinth", "projectId": "a"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("install: %d %s", rec.Code, rec.Body)
	}
	if got := listDir(t, srv, gs, token, "/plugins"); strings.Join(got, ",") != "a-1.0.jar,b-1.0.jar" {
		t.Fatalf("plugins = %v", got)
	}
}

func TestInstallSkipsDependencyPresentUnderAnotherName(t *testing.T) {
	srv, gs, token, src := newModsTestServer(t, "1.21.4")
	a, b := []byte("plugin-a"), []byte("lib-b")
	host := jarHost(t, map[string][]byte{"a1": a, "b1": b})
	src.versions["a"] = []modsource.Version{modVer(host, "a", "a1", "a-1.0.jar", a, t0, "b")}
	src.versions["b"] = []modsource.Version{modVer(host, "b", "b1", "b-1.0.jar", b, t0)}
	putFile(t, srv, gs, token, "/plugins/my-lib.jar", b)
	src.known[sha1Hex(b)] = modsource.Match{Project: modsource.Project{ID: "b", Title: "Lib B"}, Version: src.versions["b"][0]}

	rec := doRequest(t, srv, http.MethodPost, modsURL(gs, "/install"), token, map[string]string{"source": "modrinth", "projectId": "a"})
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"skipped":["b"]`) {
		t.Fatalf("install: %d %s", rec.Code, rec.Body)
	}
	if got := listDir(t, srv, gs, token, "/plugins"); strings.Join(got, ",") != "a-1.0.jar,my-lib.jar" {
		t.Fatalf("plugins = %v", got)
	}
}

func TestInstallRejectsBadDownloads(t *testing.T) {
	srv, gs, token, src := newModsTestServer(t, "1.21.4")
	content := []byte("0123456789")
	host := jarHost(t, map[string][]byte{"v1": content})
	install := func(v modsource.Version) *httptest.ResponseRecorder {
		src.versions["p"] = []modsource.Version{v}
		return doRequest(t, srv, http.MethodPost, modsURL(gs, "/install"), token, map[string]string{"source": "modrinth", "projectId": "p"})
	}

	bad := modVer(host, "p", "v1", "p.jar", content, t0)
	bad.File.SHA1 = strings.Repeat("0", 40)
	if rec := install(bad); rec.Code != http.StatusBadGateway {
		t.Errorf("hash mismatch: %d", rec.Code)
	}
	srv.modMaxBytesOverride = 4
	if rec := install(modVer(host, "p", "v1", "p.jar", content, t0)); rec.Code != http.StatusBadGateway {
		t.Errorf("too large: %d", rec.Code)
	}
	srv.modMaxBytesOverride = 0
	blocked := modVer(host, "p", "v1", "p.jar", content, t0)
	blocked.DistributionBlocked, blocked.PageURL = true, "https://www.curseforge.com/minecraft/bukkit-plugins/p"
	if rec := install(blocked); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "curseforge.com") {
		t.Errorf("blocked: %d %s", rec.Code, rec.Body)
	}
	if rec := install(modVer(host, "p", "v1", "readme.txt", content, t0)); rec.Code != http.StatusBadGateway {
		t.Errorf("not a jar: %d", rec.Code)
	}
	if got := listDir(t, srv, gs, token, "/plugins"); len(got) != 0 {
		t.Fatalf("nothing may be written, got %v", got)
	}
	if rec := install(modVer(host, "p", "v1", "../../evil.jar", content, t0)); rec.Code != http.StatusCreated {
		t.Fatalf("traversal name: %d %s", rec.Code, rec.Body)
	}
	if got := listDir(t, srv, gs, token, "/plugins"); strings.Join(got, ",") != "evil.jar" {
		t.Fatalf("plugins = %v", got)
	}
}

func TestInstallNameCollisionWithOtherProject(t *testing.T) {
	srv, gs, token, src := newModsTestServer(t, "1.21.4")
	content := []byte("new")
	host := jarHost(t, map[string][]byte{"v1": content})
	src.versions["p"] = []modsource.Version{modVer(host, "p", "v1", "shared.jar", content, t0)}
	putFile(t, srv, gs, token, "/plugins/shared.jar", []byte("something else"))
	if rec := doRequest(t, srv, http.MethodPost, modsURL(gs, "/install"), token, map[string]string{"source": "modrinth", "projectId": "p"}); rec.Code != http.StatusConflict {
		t.Fatalf("got %d, want 409", rec.Code)
	}
}

func TestInstallNeedsFilesWrite(t *testing.T) {
	srv, gs, _, _ := newModsTestServer(t, "1.21.4")
	reader := memberWithGrants(t, srv, "reader", paneldb.Grant{GameServer: gs.Name, Permissions: []paneldb.Permission{paneldb.PermFilesRead}})
	if rec := doRequest(t, srv, http.MethodPost, modsURL(gs, "/install"), reader, map[string]string{"source": "modrinth", "projectId": "p"}); rec.Code != http.StatusForbidden {
		t.Fatalf("got %d", rec.Code)
	}
}

type installedBody struct {
	Items    []installedMod `json:"items"`
	Warnings []string       `json:"warnings"`
}

func TestInstalledIdentifiesAndMarksUpdates(t *testing.T) {
	srv, gs, token, src := newModsTestServer(t, "1.21.4")
	old, newer := []byte("a-v1"), []byte("a-v2")
	host := jarHost(t, map[string][]byte{"a2": newer})
	v1 := modVer(host, "a", "a1", "a-1.jar", old, t0)
	v2 := modVer(host, "a", "a2", "a-2.jar", newer, t0.Add(time.Hour))
	src.known[sha1Hex(old)] = modsource.Match{Project: modsource.Project{ID: "a", Title: "Plugin A"}, Version: v1, Latest: &v2}
	putFile(t, srv, gs, token, "/plugins/a-1.jar", old)
	putFile(t, srv, gs, token, "/plugins/junk.jar", []byte("not a real jar"))
	curse := &fakeModSource{name: "curseforge", err: modsource.ErrUnavailable}
	srv.ModSources["curseforge"] = curse

	rec := doRequest(t, srv, http.MethodGet, modsURL(gs, "/installed"), token, nil)
	var body installedBody
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusOK || len(body.Items) != 2 {
		t.Fatalf("installed: %d %s", rec.Code, rec.Body)
	}
	byFile := map[string]installedMod{}
	for _, it := range body.Items {
		byFile[it.File] = it
	}
	if a := byFile["a-1.jar"]; a.Title != "Plugin A" || !a.UpdateAvailable || a.LatestVersion != "a2" || a.Source != "modrinth" {
		t.Errorf("a = %+v", a)
	}
	if j := byFile["junk.jar"]; j.Source != "" || j.UpdateAvailable {
		t.Errorf("junk = %+v", j)
	}
	if len(body.Warnings) != 1 || !strings.Contains(body.Warnings[0], "curseforge") {
		t.Errorf("warnings = %v", body.Warnings)
	}
	// Hashes are cached: the second listing identifies without re-reading (cache holds the key).
	before := src.identifies
	doRequest(t, srv, http.MethodGet, modsURL(gs, "/installed"), token, nil)
	if src.identifies != before+1 {
		t.Errorf("identify calls = %d", src.identifies-before)
	}
}

func TestUpdateAndRemoveMod(t *testing.T) {
	srv, gs, token, src := newModsTestServer(t, "1.21.4")
	old, newer := []byte("a-v1"), []byte("a-v2")
	host := jarHost(t, map[string][]byte{"a2": newer})
	v1 := modVer(host, "a", "a1", "a-1.jar", old, t0)
	v2 := modVer(host, "a", "a2", "a-2.jar", newer, t0.Add(time.Hour))
	src.known[sha1Hex(old)] = modsource.Match{Project: modsource.Project{ID: "a"}, Version: v1, Latest: &v2}
	src.known[sha1Hex(newer)] = modsource.Match{Project: modsource.Project{ID: "a"}, Version: v2}
	putFile(t, srv, gs, token, "/plugins/a-1.jar", old)

	if rec := doRequest(t, srv, http.MethodPost, modsURL(gs, "/update"), token, map[string]string{"file": "a-1.jar"}); rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body)
	}
	if got := listDir(t, srv, gs, token, "/plugins"); strings.Join(got, ",") != "a-2.jar" {
		t.Fatalf("after update: %v", got)
	}
	if rec := doRequest(t, srv, http.MethodPost, modsURL(gs, "/update"), token, map[string]string{"file": "a-2.jar"}); rec.Code != http.StatusConflict {
		t.Fatalf("no update available: %d", rec.Code)
	}
	if rec := doRequest(t, srv, http.MethodDelete, modsURL(gs, "/installed?file=../x.jar"), token, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("traversal remove: %d", rec.Code)
	}
	if rec := doRequest(t, srv, http.MethodDelete, modsURL(gs, "/installed?file=missing.jar"), token, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing: %d", rec.Code)
	}
	if rec := doRequest(t, srv, http.MethodDelete, modsURL(gs, "/installed?file=a-2.jar"), token, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("remove: %d %s", rec.Code, rec.Body)
	}
	if got := listDir(t, srv, gs, token, "/plugins"); len(got) != 0 {
		t.Fatalf("after remove: %v", got)
	}
}
