package panelapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/modsource"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

func TestJavaImageFor(t *testing.T) {
	for gv, want := range map[string]string{
		"26.3": "Java 25", "26.1.2": "Java 25", "1.21.4": "Java 21", "1.20.5": "Java 21",
		"1.20.4": "Java 17", "1.18.2": "Java 17", "1.17": "Java 17", "1.16.5": "Java 8", "1.12.2": "Java 8", "": "Java 25",
	} {
		if got := javaImageFor(gv); got != want {
			t.Errorf("javaImageFor(%q) = %q, want %q", gv, got, want)
		}
	}
}

// newModpackTestServer is the file-manager harness with the server's Egg ("minecraft") turned into a
// modpack Egg, the server pinned to version v1 of project "pack", and a fake "modrinth" source.
func newModpackTestServer(t *testing.T) (*Server, *gameserversv1alpha1.GameServer, string, *fakeModSource) {
	t.Helper()
	srv, gs, token := newFileManagerTestServer(t, gameserversv1alpha1.GameServerStateRunning)
	vars := []gameserversv1alpha1.EggVariable{}
	for _, n := range []string{"EULA", "HATCHERY_PACK_SOURCE", "HATCHERY_PACK", "HATCHERY_PACK_PROJECT_ID", "HATCHERY_PACK_VERSION"} {
		vars = append(vars, gameserversv1alpha1.EggVariable{Name: n, UserEditable: true})
	}
	egg := &gameserversv1alpha1.Egg{
		ObjectMeta: metav1.ObjectMeta{Name: "minecraft", Namespace: testOrgNS(), Labels: map[string]string{gameserversv1alpha1.ModpackEggLabel: "true"}},
		Spec: gameserversv1alpha1.EggSpec{
			Images: []gameserversv1alpha1.EggImage{
				{Name: "Java 25", Image: "itzg/minecraft-server:java25"}, {Name: "Java 21", Image: "itzg/minecraft-server:java21"},
				{Name: "Java 17", Image: "itzg/minecraft-server:java17"}, {Name: "Java 8", Image: "itzg/minecraft-server:java8"},
			},
			StartCommand: "/image/scripts/start",
			Variables:    vars,
		},
	}
	if err := srv.Client.Create(context.Background(), egg); err != nil {
		t.Fatal(err)
	}
	var cur gameserversv1alpha1.GameServer
	_ = srv.Client.Get(context.Background(), gameServerKeyOf(gs), &cur)
	cur.Spec.ImageName = "Java 21"
	cur.Spec.Variables = []gameserversv1alpha1.GameServerVariable{
		{Name: "HATCHERY_PACK_SOURCE", Value: "modrinth"}, {Name: "HATCHERY_PACK", Value: "pack"},
		{Name: "HATCHERY_PACK_PROJECT_ID", Value: "pack"}, {Name: "HATCHERY_PACK_VERSION", Value: "v1"},
	}
	if err := srv.Client.Update(context.Background(), &cur); err != nil {
		t.Fatal(err)
	}
	src := &fakeModSource{name: "modrinth", versions: map[string][]modsource.Version{
		"pack": {
			{ID: "v2", ProjectID: "pack", VersionNumber: "2.0", GameVersions: []string{"26.3"}, Loaders: []string{"neoforge"}, PublishedAt: t0.Add(time.Hour)},
			{ID: "v1", ProjectID: "pack", VersionNumber: "1.0", GameVersions: []string{"1.21.1"}, Loaders: []string{"neoforge"}, PublishedAt: t0},
		},
	}}
	srv.ModSources = map[string]modsource.Source{"modrinth": src}
	return srv, &cur, token, src
}

func TestModpackSearchAndVersions(t *testing.T) {
	srv, _, token, src := newModpackTestServer(t)
	rec := doRequest(t, srv, http.MethodGet, orgURL("/modpacks/search?source=modrinth&q=cobble"), token, nil)
	if rec.Code != http.StatusOK || src.lastSearch.Kind != modsource.KindModpack || src.lastSearch.Query != "cobble" {
		t.Fatalf("search: %d %s %+v", rec.Code, rec.Body, src.lastSearch)
	}
	rec = doRequest(t, srv, http.MethodGet, orgURL("/modpacks/modrinth/pack/versions"), token, nil)
	var vs []modpackVersion
	_ = json.Unmarshal(rec.Body.Bytes(), &vs)
	if rec.Code != http.StatusOK || len(vs) != 2 || vs[0].ImageName != "Java 25" || vs[1].ImageName != "Java 21" || vs[1].GameVersion != "1.21.1" {
		t.Fatalf("versions: %d %s", rec.Code, rec.Body)
	}
	member := newMemberToken(t, srv, "member-mp", paneldb.RoleMember)
	if got := doRequest(t, srv, http.MethodGet, orgURL("/modpacks/search?source=modrinth&q=x"), member, nil).Code; got != http.StatusForbidden {
		t.Errorf("member searching modpacks (creating servers is admin+): %d", got)
	}
}

func TestModpackStatusAndUpdate(t *testing.T) {
	srv, gs, token, _ := newModpackTestServer(t)
	base := orgURL("/gameservers/" + gs.Name + "/modpack")
	rec := doRequest(t, srv, http.MethodGet, base, token, nil)
	var st modpackStatus
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if rec.Code != http.StatusOK || st.Version == nil || st.Version.ID != "v1" || st.Latest == nil || st.Latest.ID != "v2" || !st.UpdateAvailable {
		t.Fatalf("status: %d %s", rec.Code, rec.Body)
	}

	if got := doRequest(t, srv, http.MethodPost, base+"/update", token, map[string]string{"versionId": "nope"}).Code; got != http.StatusUnprocessableEntity {
		t.Fatalf("unknown version: %d", got)
	}
	rec = doRequest(t, srv, http.MethodPost, base+"/update", token, map[string]string{"versionId": "v2"})
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body)
	}
	var cur gameserversv1alpha1.GameServer
	_ = srv.Client.Get(context.Background(), gameServerKeyOf(gs), &cur)
	got := map[string]string{}
	for _, v := range cur.Spec.Variables {
		got[v.Name] = v.Value
	}
	if got["HATCHERY_PACK_VERSION"] != "v2" || cur.Spec.ImageName != "Java 25" || cur.Annotations[gameserversv1alpha1.RestartAnnotation] == "" {
		t.Fatalf("after update: vars=%v image=%q annotations=%v", got, cur.Spec.ImageName, cur.Annotations)
	}

	member := memberWithGrants(t, srv, "power", paneldb.Grant{GameServer: gs.Name, Permissions: []paneldb.Permission{paneldb.PermPower}})
	if code := doRequest(t, srv, http.MethodGet, base, member, nil).Code; code != http.StatusOK {
		t.Errorf("any grant sees the modpack card: %d", code)
	}
	if code := doRequest(t, srv, http.MethodPost, base+"/update", member, map[string]string{"versionId": "v1"}).Code; code != http.StatusForbidden {
		t.Errorf("member updating the pack: %d", code)
	}
}

func TestModpackStatusNotAModpackServer(t *testing.T) {
	srv, gs, token, _ := newModsTestServer(t, "1.21.4") // plain plugin Egg
	if got := doRequest(t, srv, http.MethodGet, orgURL("/gameservers/"+gs.Name+"/modpack"), token, nil).Code; got != http.StatusNotFound {
		t.Fatalf("got %d, want 404", got)
	}
}
