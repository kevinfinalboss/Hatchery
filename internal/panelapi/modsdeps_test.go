package panelapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/modsource"
)

func testJar(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(content))
	}
	_ = zw.Close()
	return buf.Bytes()
}

// makeFabric turns the test Egg into a Fabric one (mods in /mods).
func makeFabric(t *testing.T, srv *Server, gs *gameserversv1alpha1.GameServer) {
	t.Helper()
	var egg gameserversv1alpha1.Egg
	if err := srv.Client.Get(context.Background(), gameServerEggKey(gs), &egg); err != nil {
		t.Fatal(err)
	}
	egg.Spec.Mods.Kind, egg.Spec.Mods.Loaders, egg.Spec.Mods.Directory = gameserversv1alpha1.EggModsMod, []string{"fabric"}, "mods"
	if err := srv.Client.Update(context.Background(), &egg); err != nil {
		t.Fatal(err)
	}
}

type installBody struct {
	Installed []installedEntry `json:"installed"`
	Warnings  []string         `json:"warnings"`
}

func TestInstallResolvesDependenciesDeclaredOnlyInTheJar(t *testing.T) {
	srv, gs, token, src := newModsTestServer(t, "26.3")
	makeFabric(t, srv, gs)
	spark := testJar(t, map[string]string{"fabric.mod.json": `{"id":"spark","depends":{"fabricloader":"*","fabric-api-base":"*"}}`})
	api := testJar(t, map[string]string{
		"fabric.mod.json":        `{"id":"fabric-api","jars":[{"file":"META-INF/jars/base.jar"}]}`,
		"META-INF/jars/base.jar": string(testJar(t, map[string]string{"fabric.mod.json": `{"id":"fabric-api-base"}`})),
	})
	host := jarHost(t, map[string][]byte{"spark1": spark, "api1": api})
	src.versions["spark"] = []modsource.Version{modVer(host, "spark", "spark1", "spark.jar", spark, t0)}
	src.versions["fabric-api"] = []modsource.Version{modVer(host, "fabric-api", "api1", "fabric-api.jar", api, t0)}

	rec := doRequest(t, srv, http.MethodPost, modsURL(gs, "/install"), token, map[string]string{"source": "modrinth", "projectId": "spark"})
	var body installBody
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusCreated || len(body.Installed) != 2 || len(body.Warnings) != 0 {
		t.Fatalf("install: %d %s", rec.Code, rec.Body)
	}
	if got := listDir(t, srv, gs, token, "/mods"); strings.Join(got, ",") != "fabric-api.jar,spark.jar" {
		t.Fatalf("mods = %v", got)
	}

	// Fabric API is now installed: another mod needing a Fabric API module brings nothing more.
	other := testJar(t, map[string]string{"fabric.mod.json": `{"id":"other","depends":{"fabric-api-base":"*"}}`})
	host2 := jarHost(t, map[string][]byte{"o1": other})
	src.versions["other"] = []modsource.Version{modVer(host2, "other", "o1", "other.jar", other, t0)}
	rec = doRequest(t, srv, http.MethodPost, modsURL(gs, "/install"), token, map[string]string{"source": "modrinth", "projectId": "other"})
	body = installBody{}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusCreated || len(body.Installed) != 1 || len(body.Warnings) != 0 {
		t.Fatalf("second install: %d %s", rec.Code, rec.Body)
	}
}

func TestInstallWarnsAboutUnresolvableJarDependency(t *testing.T) {
	srv, gs, token, src := newModsTestServer(t, "26.3")
	makeFabric(t, srv, gs)
	lonely := testJar(t, map[string]string{"fabric.mod.json": `{"id":"lonely","depends":{"mystery-lib":"*"}}`})
	host := jarHost(t, map[string][]byte{"l1": lonely})
	src.versions["lonely"] = []modsource.Version{modVer(host, "lonely", "l1", "lonely.jar", lonely, t0)}

	rec := doRequest(t, srv, http.MethodPost, modsURL(gs, "/install"), token, map[string]string{"source": "modrinth", "projectId": "lonely"})
	var body installBody
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusCreated || len(body.Installed) != 1 || len(body.Warnings) != 1 || !strings.Contains(body.Warnings[0], "mystery-lib") {
		t.Fatalf("install: %d %s", rec.Code, rec.Body)
	}
}

func TestInstallWarnsAboutMissingPluginDepend(t *testing.T) {
	srv, gs, token, src := newModsTestServer(t, "26.3")
	shop := testJar(t, map[string]string{"plugin.yml": "name: Shop\ndepend: [Vault]\n"})
	host := jarHost(t, map[string][]byte{"s1": shop})
	src.versions["shop"] = []modsource.Version{modVer(host, "shop", "s1", "Shop.jar", shop, t0)}

	rec := doRequest(t, srv, http.MethodPost, modsURL(gs, "/install"), token, map[string]string{"source": "modrinth", "projectId": "shop"})
	var body installBody
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusCreated || len(body.Warnings) != 1 || !strings.Contains(body.Warnings[0], "Vault") {
		t.Fatalf("install: %d %s", rec.Code, rec.Body)
	}
	// With Vault present there is nothing to warn about (reinstalling Shop over itself).
	src.known[sha1Hex(shop)] = modsource.Match{Project: modsource.Project{ID: "shop"}, Version: src.versions["shop"][0]}
	putFile(t, srv, gs, token, "/plugins/Vault.jar", testJar(t, map[string]string{"plugin.yml": "name: Vault\n"}))
	rec = doRequest(t, srv, http.MethodPost, modsURL(gs, "/install"), token, map[string]string{"source": "modrinth", "projectId": "shop"})
	body = installBody{}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusCreated || len(body.Warnings) != 0 {
		t.Fatalf("Vault installed: %d %s", rec.Code, rec.Body)
	}
}
