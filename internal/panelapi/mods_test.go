package panelapi

import (
	"context"
	"encoding/json"
	"net/http"
	"path"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/modsource"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

// fakeModSource is an in-memory modsource.Source. versions maps project -> versions (newest first);
// known maps sha1 -> match for Identify.
type fakeModSource struct {
	name       string
	err        error
	lastSearch modsource.SearchQuery
	lastFilter modsource.Filter
	versions   map[string][]modsource.Version
	known      map[string]modsource.Match
	identifies int
}

func (f *fakeModSource) Name() string { return f.name }
func (f *fakeModSource) Search(_ context.Context, q modsource.SearchQuery) (modsource.SearchPage, error) {
	f.lastSearch = q
	if f.err != nil {
		return modsource.SearchPage{}, f.err
	}
	return modsource.SearchPage{Total: 1, Results: []modsource.SearchResult{{Source: f.name, ProjectID: "p1", Title: "Plugin One"}}}, nil
}
func (f *fakeModSource) Versions(_ context.Context, projectID string, fl modsource.Filter) ([]modsource.Version, error) {
	f.lastFilter = fl
	if f.err != nil {
		return nil, f.err
	}
	return f.versions[projectID], nil
}
func (f *fakeModSource) Identify(_ context.Context, files []modsource.FileHashes, _ modsource.Filter) (map[string]modsource.Match, error) {
	f.identifies++
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]modsource.Match{}
	for _, fh := range files {
		if m, ok := f.known[fh.SHA1]; ok {
			out[fh.SHA1] = m
		}
	}
	return out, nil
}

// newModsTestServer is the file-manager harness plus an Egg "minecraft" with a mods block whose
// MINECRAFT_VERSION default is versionDefault, and one fake source named "modrinth".
func newModsTestServer(t *testing.T, versionDefault string) (*Server, *gameserversv1alpha1.GameServer, string, *fakeModSource) {
	t.Helper()
	srv, gs, token := newFileManagerTestServer(t, gameserversv1alpha1.GameServerStateRunning)
	egg := &gameserversv1alpha1.Egg{
		ObjectMeta: metav1.ObjectMeta{Name: "minecraft", Namespace: testOrgNS()},
		Spec: gameserversv1alpha1.EggSpec{
			Images:       []gameserversv1alpha1.EggImage{{Name: "java", Image: "ghcr.io/ptero-eggs/yolks:java_21"}},
			StartCommand: "java -jar server.jar",
			Variables:    []gameserversv1alpha1.EggVariable{{Name: "MINECRAFT_VERSION", Default: versionDefault}},
			Mods: &gameserversv1alpha1.EggMods{
				Kind: gameserversv1alpha1.EggModsPlugin, Loaders: []string{"paper", "spigot"}, Directory: "plugins",
				GameVersion: gameserversv1alpha1.EggModsGameVersion{Variable: "MINECRAFT_VERSION", File: ".hatchery/game-version"},
			},
		},
	}
	if err := srv.Client.Create(context.Background(), egg); err != nil {
		t.Fatal(err)
	}
	src := &fakeModSource{name: "modrinth", versions: map[string][]modsource.Version{}, known: map[string]modsource.Match{}}
	srv.ModSources = map[string]modsource.Source{"modrinth": src}
	return srv, gs, token, src
}

func modsURL(gs *gameserversv1alpha1.GameServer, rest string) string {
	return orgURL("/gameservers/" + gs.Name + "/mods" + rest)
}

func putFile(t *testing.T, srv *Server, gs *gameserversv1alpha1.GameServer, token, p string, content []byte) {
	t.Helper()
	if dir := path.Dir(p); dir != "/" {
		doRequest(t, srv, http.MethodPost, orgURL("/gameservers/"+gs.Name+"/files/mkdir"), token, mkdirRequest{Path: dir})
	}
	if rec := doRawRequest(t, srv, http.MethodPut, orgURL("/gameservers/"+gs.Name+"/files/content?path="+p), token, content); rec.Code != http.StatusNoContent {
		t.Fatalf("writing %s: %d %s", p, rec.Code, rec.Body)
	}
}

type modsContextBody struct {
	Kind        string   `json:"kind"`
	Loaders     []string `json:"loaders"`
	Directory   string   `json:"directory"`
	GameVersion *string  `json:"gameVersion"`
	Sources     []string `json:"sources"`
}

func getModsContext(t *testing.T, srv *Server, gs *gameserversv1alpha1.GameServer, token string) modsContextBody {
	t.Helper()
	rec := doRequest(t, srv, http.MethodGet, modsURL(gs, "/context"), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("context: %d %s", rec.Code, rec.Body)
	}
	var b modsContextBody
	_ = json.Unmarshal(rec.Body.Bytes(), &b)
	return b
}

func TestModsContextGameVersion(t *testing.T) {
	srv, gs, token, _ := newModsTestServer(t, "1.21.4")
	c := getModsContext(t, srv, gs, token)
	if c.Kind != "plugin" || c.Directory != "plugins" || c.GameVersion == nil || *c.GameVersion != "1.21.4" || len(c.Sources) != 1 || c.Sources[0] != "modrinth" {
		t.Fatalf("concrete variable: %+v", c)
	}

	srv, gs, token, _ = newModsTestServer(t, "latest")
	if c := getModsContext(t, srv, gs, token); c.GameVersion != nil {
		t.Fatalf("latest without file must be unknown, got %v", *c.GameVersion)
	}
	putFile(t, srv, gs, token, "/.hatchery/game-version", []byte("26.1\n"))
	if c := getModsContext(t, srv, gs, token); c.GameVersion == nil || *c.GameVersion != "26.1" {
		t.Fatalf("latest with file: %+v", c)
	}
}

func TestModsNotAvailableWithoutModsBlock(t *testing.T) {
	srv, gs, token, _ := newModsTestServer(t, "1.21.4")
	var egg gameserversv1alpha1.Egg
	_ = srv.Client.Get(context.Background(), gameServerEggKey(gs), &egg)
	egg.Spec.Mods = nil
	_ = srv.Client.Update(context.Background(), &egg)
	if rec := doRequest(t, srv, http.MethodGet, modsURL(gs, "/context"), token, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", rec.Code)
	}
}

func TestModsSearchPassesFilter(t *testing.T) {
	srv, gs, token, src := newModsTestServer(t, "1.21.4")
	rec := doRequest(t, srv, http.MethodGet, modsURL(gs, "/search?source=modrinth&q=luck&page=2"), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("search: %d %s", rec.Code, rec.Body)
	}
	q := src.lastSearch
	if q.Query != "luck" || q.Page != 2 || q.Kind != modsource.KindPlugin || q.GameVersion != "1.21.4" || len(q.Loaders) != 2 {
		t.Fatalf("query = %+v", q)
	}
	doRequest(t, srv, http.MethodGet, modsURL(gs, "/search?source=modrinth&q=x&gameVersion=1.20.1"), token, nil)
	if src.lastSearch.GameVersion != "1.20.1" {
		t.Fatalf("gameVersion override: %+v", src.lastSearch)
	}
	if rec := doRequest(t, srv, http.MethodGet, modsURL(gs, "/search?source=curseforge&q=x"), token, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("unconfigured source: %d", rec.Code)
	}
}

func TestModsSearchSourceErrors(t *testing.T) {
	srv, gs, token, src := newModsTestServer(t, "1.21.4")
	src.err = modsource.ErrUnavailable
	if rec := doRequest(t, srv, http.MethodGet, modsURL(gs, "/search?source=modrinth&q=x"), token, nil); rec.Code != http.StatusBadGateway {
		t.Fatalf("unavailable: %d", rec.Code)
	}
	src.err = &modsource.RateLimitedError{RetryAfter: 7 * time.Second}
	rec := doRequest(t, srv, http.MethodGet, modsURL(gs, "/search?source=modrinth&q=x"), token, nil)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "7" {
		t.Fatalf("rate limited: %d %q", rec.Code, rec.Header().Get("Retry-After"))
	}
}

func TestModsVersions(t *testing.T) {
	srv, gs, token, src := newModsTestServer(t, "1.21.4")
	src.versions["p1"] = []modsource.Version{{ID: "v2"}, {ID: "v1"}}
	rec := doRequest(t, srv, http.MethodGet, modsURL(gs, "/projects/modrinth/p1/versions"), token, nil)
	var vs []modsource.Version
	_ = json.Unmarshal(rec.Body.Bytes(), &vs)
	if rec.Code != http.StatusOK || len(vs) != 2 || src.lastFilter.GameVersion != "1.21.4" {
		t.Fatalf("versions: %d %s", rec.Code, rec.Body)
	}
}

func TestModsPermissionsAndSuspension(t *testing.T) {
	srv, gs, _, _ := newModsTestServer(t, "1.21.4")
	console := memberWithGrants(t, srv, "console-only", paneldb.Grant{GameServer: gs.Name, Permissions: []paneldb.Permission{paneldb.PermConsoleRead}})
	if rec := doRequest(t, srv, http.MethodGet, modsURL(gs, "/context"), console, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("without files.read: %d", rec.Code)
	}
	reader := memberWithGrants(t, srv, "reader", paneldb.Grant{GameServer: gs.Name, Permissions: []paneldb.Permission{paneldb.PermFilesRead}})
	if rec := doRequest(t, srv, http.MethodGet, modsURL(gs, "/context"), reader, nil); rec.Code != http.StatusOK {
		t.Fatalf("with files.read: %d", rec.Code)
	}
	var cur gameserversv1alpha1.GameServer
	_ = srv.Client.Get(context.Background(), gameServerKeyOf(gs), &cur)
	cur.Spec.Suspended = true
	_ = srv.Client.Update(context.Background(), &cur)
	if rec := doRequest(t, srv, http.MethodGet, modsURL(gs, "/context"), reader, nil); rec.Code != http.StatusLocked {
		t.Fatalf("suspended: %d", rec.Code)
	}
}

func gameServerKeyOf(gs *gameserversv1alpha1.GameServer) client.ObjectKey {
	return client.ObjectKey{Namespace: gs.Namespace, Name: gs.Name}
}

func gameServerEggKey(gs *gameserversv1alpha1.GameServer) client.ObjectKey {
	return client.ObjectKey{Namespace: gs.EggNamespace(), Name: gs.Spec.EggRef.Name}
}

// While the server restarts the sftp-agent is gone: the mods tab falls back to the last game
// version it read instead of failing, and writes say the files are not reachable right now.
func TestModsWhileServerFilesUnreachable(t *testing.T) {
	srv, gs, token, _ := newModsTestServer(t, "latest")
	reachable := srv.resolveSFTPAddr
	putFile(t, srv, gs, token, "/.hatchery/game-version", []byte("26.1\n"))
	if c := getModsContext(t, srv, gs, token); c.GameVersion == nil || *c.GameVersion != "26.1" {
		t.Fatalf("reachable: %+v", c)
	}

	srv.resolveSFTPAddr = func(_, _ string) string { return "127.0.0.1:1" }
	if c := getModsContext(t, srv, gs, token); c.GameVersion == nil || *c.GameVersion != "26.1" {
		t.Fatalf("unreachable must use the remembered version: %+v", c)
	}
	if rec := doRequest(t, srv, http.MethodGet, modsURL(gs, "/search?source=modrinth&q=x"), token, nil); rec.Code != http.StatusOK {
		t.Fatalf("search while unreachable: %d %s", rec.Code, rec.Body)
	}
	rec := doRequest(t, srv, http.MethodPost, modsURL(gs, "/install"), token, map[string]string{"source": "modrinth", "projectId": "p"})
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "not reachable") {
		t.Fatalf("install while unreachable: %d %s", rec.Code, rec.Body)
	}

	// A server never read before: unknown version, not an error.
	srv2, gs2, token2, _ := newModsTestServer(t, "latest")
	srv2.resolveSFTPAddr = func(_, _ string) string { return "127.0.0.1:1" }
	if c := getModsContext(t, srv2, gs2, token2); c.GameVersion != nil {
		t.Fatalf("never read: %+v", c)
	}
	_ = reachable
}
