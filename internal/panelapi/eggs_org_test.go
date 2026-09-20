package panelapi

import (
	"encoding/json"
	"net/http"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

func egg(name, ns string) *gameserversv1alpha1.Egg {
	return &gameserversv1alpha1.Egg{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       gameserversv1alpha1.EggSpec{Image: "example.com/" + name + ":1", StartCommand: "run"},
	}
}

var eggBody = map[string]any{"name": "mine", "spec": map[string]any{"image": "example.com/mine:1", "startCommand": "run"}}

type eggListItem struct {
	Name  string `json:"name"`
	Scope string `json:"scope"`
}

func TestOrgEggListIsCatalogPlusOwnPrivateOnly(t *testing.T) {
	srv := newTestServer(t,
		egg("official", gameserversv1alpha1.CatalogNamespace),
		egg("ours", testOrgNS()),
		egg("theirs", gameserversv1alpha1.TenantNamespace("otherorg")),
	)
	member := newMemberToken(t, srv, "m", paneldb.RoleMember)

	rec := doRequest(t, srv, http.MethodGet, orgURL("/eggs"), member, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	var got []eggListItem
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, e := range got {
		seen[e.Name] = e.Scope
	}
	if len(seen) != 2 || seen["official"] != "Catalog" || seen["ours"] != "Namespace" {
		t.Fatalf("want the catalog Egg and this org's own only, got %v", seen)
	}
}

func TestOrgEggReadByScopeNeverReachesAnotherOrg(t *testing.T) {
	srv := newTestServer(t,
		egg("official", gameserversv1alpha1.CatalogNamespace),
		egg("theirs", gameserversv1alpha1.TenantNamespace("otherorg")),
	)
	member := newMemberToken(t, srv, "m", paneldb.RoleMember)

	if rec := doRequest(t, srv, http.MethodGet, orgURL("/eggs/catalog/official"), member, nil); rec.Code != http.StatusOK {
		t.Errorf("catalog egg: got %d", rec.Code)
	}
	// "private" always means THIS org's namespace, so another org's Egg is simply not there.
	if rec := doRequest(t, srv, http.MethodGet, orgURL("/eggs/private/theirs"), member, nil); rec.Code != http.StatusNotFound {
		t.Errorf("another org's private egg: got %d, want 404", rec.Code)
	}
	if rec := doRequest(t, srv, http.MethodGet, orgURL("/eggs/whatever/official"), member, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown scope segment: got %d, want 400", rec.Code)
	}
}

func TestPrivateEggWriteRequiresOrgAdmin(t *testing.T) {
	srv := newTestServer(t)
	member := newMemberToken(t, srv, "m", paneldb.RoleMember)
	adminTok := newMemberToken(t, srv, "a", paneldb.RoleAdmin)

	if rec := doRequest(t, srv, http.MethodPost, orgURL("/eggs"), member, eggBody); rec.Code != http.StatusForbidden {
		t.Fatalf("member create: got %d, want 403", rec.Code)
	}
	if rec := doRequest(t, srv, http.MethodPost, orgURL("/eggs"), adminTok, eggBody); rec.Code != http.StatusCreated {
		t.Fatalf("admin create: got %d: %s", rec.Code, rec.Body.String())
	}
	var created gameserversv1alpha1.Egg
	if err := srv.Client.Get(t.Context(), client.ObjectKey{Namespace: testOrgNS(), Name: "mine"}, &created); err != nil {
		t.Fatalf("the Egg must land in the org's own namespace: %v", err)
	}
	if rec := doRequest(t, srv, http.MethodPost, orgURL("/eggs"), adminTok, eggBody); rec.Code != http.StatusConflict {
		t.Fatalf("duplicate: got %d, want 409", rec.Code)
	}

	upd := map[string]any{"spec": map[string]any{"image": "example.com/mine:2", "startCommand": "run2"}}
	if rec := doRequest(t, srv, http.MethodPut, orgURL("/eggs/mine"), adminTok, upd); rec.Code != http.StatusOK {
		t.Fatalf("update: got %d: %s", rec.Code, rec.Body.String())
	}
	if rec := doRequest(t, srv, http.MethodDelete, orgURL("/eggs/mine"), adminTok, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: got %d", rec.Code)
	}
}

func TestDeletingAPrivateEggInUseIsRefused(t *testing.T) {
	gs := &gameserversv1alpha1.GameServer{
		ObjectMeta: metav1.ObjectMeta{Name: "uses-it", Namespace: testOrgNS()},
		Spec: gameserversv1alpha1.GameServerSpec{
			EggRef: gameserversv1alpha1.GameServerEggRef{Name: "busy", Scope: gameserversv1alpha1.EggScopeNamespace},
		},
	}
	srv := newTestServer(t, egg("busy", testOrgNS()), gs)
	adminTok := newMemberToken(t, srv, "a", paneldb.RoleAdmin)
	if rec := doRequest(t, srv, http.MethodDelete, orgURL("/eggs/busy"), adminTok, nil); rec.Code != http.StatusConflict {
		t.Fatalf("got %d, want 409 while a GameServer uses the Egg: %s", rec.Code, rec.Body.String())
	}
}

func TestCatalogWriteIsPlatformAdminOnly(t *testing.T) {
	srv := newTestServer(t)
	owner := newMemberToken(t, srv, "o", paneldb.RoleOwner)
	platform := adminToken(t, srv)

	if rec := doRequest(t, srv, http.MethodPost, "/api/v1/catalog/eggs", owner, eggBody); rec.Code != http.StatusForbidden {
		t.Fatalf("an org owner must not write the catalog: got %d", rec.Code)
	}
	if rec := doRequest(t, srv, http.MethodPost, "/api/v1/catalog/eggs", platform, eggBody); rec.Code != http.StatusCreated {
		t.Fatalf("platform admin: got %d: %s", rec.Code, rec.Body.String())
	}
	var created gameserversv1alpha1.Egg
	if err := srv.Client.Get(t.Context(), client.ObjectKey{Namespace: gameserversv1alpha1.CatalogNamespace, Name: "mine"}, &created); err != nil {
		t.Fatalf("the Egg must land in the catalog namespace: %v", err)
	}
	if rec := doRequest(t, srv, http.MethodGet, "/api/v1/catalog/eggs", owner, nil); rec.Code != http.StatusOK {
		t.Fatalf("any authenticated user can read the catalog: got %d", rec.Code)
	}
}

func TestEggActionsAreAudited(t *testing.T) {
	srv := newTestServer(t)
	adminTok := newMemberToken(t, srv, "a", paneldb.RoleAdmin)
	doRequest(t, srv, http.MethodPost, orgURL("/eggs"), adminTok, eggBody)
	doRequest(t, srv, http.MethodPost, "/api/v1/catalog/eggs", adminToken(t, srv), eggBody)

	org := listOrgAudit(t, srv, testOrgSlug)
	if len(org) != 1 || org[0].Action != "egg.create" || org[0].TargetName != "mine" {
		t.Fatalf("org events = %+v", org)
	}
	platform, _ := srv.DB.ListAudit(t.Context(), "", 10, 0)
	found := false
	for _, e := range platform {
		if e.Action == "catalog.egg.create" && e.TargetName == "mine" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a catalog.egg.create platform event, got %+v", platform)
	}
}
