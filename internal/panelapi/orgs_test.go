package panelapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

var goodQuota = map[string]any{"cpu": "4", "memory": "8Gi", "storage": "50Gi", "maxGameServers": 3}

func TestCreateOrgCreatesRowMembershipAndTenant(t *testing.T) {
	srv := newTestServer(t)
	admin := adminToken(t, srv)
	if _, err := srv.DB.CreateUser(t.Context(), "future-owner", "password", false); err != nil {
		t.Fatal(err)
	}

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/orgs", admin, map[string]any{
		"slug": "acme", "name": "Acme Games", "ownerUsername": "future-owner", "quota": goodQuota,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}

	var tenant gameserversv1alpha1.Tenant
	if err := srv.Client.Get(t.Context(), client.ObjectKey{Name: "acme"}, &tenant); err != nil {
		t.Fatalf("a Tenant CR named after the slug must exist: %v", err)
	}
	if tenant.Spec.Quota.MaxGameServers != 3 {
		t.Errorf("tenant quota = %+v", tenant.Spec.Quota)
	}
	org, err := srv.DB.GetOrgBySlug(t.Context(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	owner, _ := srv.DB.GetUserByUsername(t.Context(), "future-owner")
	if role, err := srv.DB.GetMembership(t.Context(), org.ID, owner.ID); err != nil || role != paneldb.RoleOwner {
		t.Fatalf("owner membership = (%q, %v)", role, err)
	}
}

func TestCreateOrgValidation(t *testing.T) {
	srv := newTestServer(t)
	admin := adminToken(t, srv)
	if _, err := srv.DB.CreateUser(t.Context(), "o", "password", false); err != nil {
		t.Fatal(err)
	}
	post := func(body map[string]any) int {
		return doRequest(t, srv, http.MethodPost, "/api/v1/orgs", admin, body).Code
	}
	if got := post(map[string]any{"slug": "Bad Slug", "name": "x", "ownerUsername": "o", "quota": goodQuota}); got != http.StatusBadRequest {
		t.Errorf("invalid slug: got %d, want 400", got)
	}
	for _, reserved := range []string{"system", "catalog"} {
		if got := post(map[string]any{"slug": reserved, "name": "x", "ownerUsername": "o", "quota": goodQuota}); got != http.StatusBadRequest {
			t.Errorf("reserved slug %q: got %d, want 400", reserved, got)
		}
	}
	if got := post(map[string]any{"slug": "ok", "name": "x", "ownerUsername": "ghost", "quota": goodQuota}); got != http.StatusNotFound {
		t.Errorf("unknown owner: got %d, want 404", got)
	}
	if got := post(map[string]any{"slug": "ok", "name": "x", "ownerUsername": "o", "quota": map[string]any{"cpu": "4"}}); got != http.StatusBadRequest {
		t.Errorf("incomplete quota: got %d, want 400", got)
	}
	if got := post(map[string]any{"slug": testOrgSlug, "name": "dup", "ownerUsername": "o", "quota": goodQuota}); got != http.StatusConflict {
		t.Errorf("duplicate slug: got %d, want 409", got)
	}
}

func TestOnlyPlatformAdminCreatesOrgs(t *testing.T) {
	srv := newTestServer(t)
	owner := newMemberToken(t, srv, "orgowner", paneldb.RoleOwner)
	if rec := doRequest(t, srv, http.MethodPost, "/api/v1/orgs", owner, map[string]any{"slug": "x"}); rec.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", rec.Code)
	}
}

func TestListOrgsShowsOnlyTheUsersOrgsExceptForPlatformAdmin(t *testing.T) {
	srv := newTestServer(t)
	member := newMemberToken(t, srv, "m", paneldb.RoleMember)
	other, _ := srv.DB.CreateUser(t.Context(), "other-owner", "password", false)
	if _, err := srv.DB.CreateOrg(t.Context(), "secretorg", "Secret", other.ID); err != nil {
		t.Fatal(err)
	}

	rec := doRequest(t, srv, http.MethodGet, "/api/v1/orgs", member, nil)
	var mine []struct{ Slug, Role string }
	if err := json.Unmarshal(rec.Body.Bytes(), &mine); err != nil {
		t.Fatal(err)
	}
	if len(mine) != 1 || mine[0].Slug != testOrgSlug || mine[0].Role != "member" {
		t.Fatalf("a member must see only their own org with their role, got %+v", mine)
	}

	rec = doRequest(t, srv, http.MethodGet, "/api/v1/orgs", adminToken(t, srv), nil)
	var all []struct{ Slug string }
	_ = json.Unmarshal(rec.Body.Bytes(), &all)
	if len(all) != 2 {
		t.Fatalf("a platform admin sees every org, got %+v", all)
	}
}

func TestDeleteOrgIsBlockedWhileServersRemain(t *testing.T) {
	gs := &gameserversv1alpha1.GameServer{}
	gs.Name, gs.Namespace = "still-here", testOrgNS()
	srv := newTestServer(t, gs, tenantWithQuota(3, "10Gi"))
	owner := newMemberToken(t, srv, "own", paneldb.RoleOwner)

	if rec := doRequest(t, srv, http.MethodDelete, orgURL(""), owner, nil); rec.Code != http.StatusConflict {
		t.Fatalf("got %d, want 409 while a GameServer remains: %s", rec.Code, rec.Body.String())
	}
	if _, err := srv.DB.GetOrgBySlug(t.Context(), testOrgSlug); err != nil {
		t.Fatal("the org row must survive a blocked delete")
	}
}

func TestDeleteEmptyOrgRemovesTenantAndRow(t *testing.T) {
	srv := newTestServer(t, tenantWithQuota(3, "10Gi"))
	owner := newMemberToken(t, srv, "own", paneldb.RoleOwner)
	adminRole := newMemberToken(t, srv, "adm", paneldb.RoleAdmin)

	if rec := doRequest(t, srv, http.MethodDelete, orgURL(""), adminRole, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("only an owner deletes the org: got %d", rec.Code)
	}
	if rec := doRequest(t, srv, http.MethodDelete, orgURL(""), owner, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := srv.DB.GetOrgBySlug(t.Context(), testOrgSlug); err == nil {
		t.Fatal("org row should be gone")
	}
	var tenant gameserversv1alpha1.Tenant
	if err := srv.Client.Get(t.Context(), client.ObjectKey{Name: testOrgSlug}, &tenant); !errors.IsNotFound(err) {
		t.Fatalf("Tenant CR should be deleted, got err=%v", err)
	}
}

func TestUpdateQuotaIsPlatformAdminOnly(t *testing.T) {
	srv := newTestServer(t, tenantWithQuota(3, "10Gi"))
	owner := newMemberToken(t, srv, "own", paneldb.RoleOwner)
	newQuota := map[string]any{"cpu": "8", "memory": "16Gi", "storage": "200Gi", "maxGameServers": 9}

	if rec := doRequest(t, srv, http.MethodPatch, orgURL("/quota"), owner, newQuota); rec.Code != http.StatusForbidden {
		t.Fatalf("an org owner must not change quota: got %d", rec.Code)
	}
	if rec := doRequest(t, srv, http.MethodPatch, orgURL("/quota"), adminToken(t, srv), newQuota); rec.Code != http.StatusOK {
		t.Fatalf("platform admin: got %d: %s", rec.Code, rec.Body.String())
	}
	var tenant gameserversv1alpha1.Tenant
	if err := srv.Client.Get(t.Context(), client.ObjectKey{Name: testOrgSlug}, &tenant); err != nil {
		t.Fatal(err)
	}
	if tenant.Spec.Quota.MaxGameServers != 9 {
		t.Fatalf("quota not updated: %+v", tenant.Spec.Quota)
	}
}
