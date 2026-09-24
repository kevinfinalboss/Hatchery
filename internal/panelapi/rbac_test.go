package panelapi

import (
	"os"
	"slices"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

// TestPanelTenantRoleCoversPanelResources guards config/rbac/panel_tenant_role.yaml, which is
// what the panel gets in every org namespace. In dev the panel may run with an admin
// kubeconfig, so a resource missing here only shows up once deployed (it happened with
// gameserverschedules: every schedules route answered 403 from the apiserver).
func TestPanelTenantRoleCoversPanelResources(t *testing.T) {
	raw, err := os.ReadFile("../../config/rbac/panel_tenant_role.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var role rbacv1.ClusterRole
	if err := yaml.Unmarshal(raw, &role); err != nil {
		t.Fatal(err)
	}
	for _, res := range []string{"gameservers", "gameserverbackups", "gameserverrestores", "gameserverschedules", "eggs"} {
		for _, verb := range []string{"get", "list", "create", "update", "delete"} {
			if !roleAllows(role, "gameservers.hatchery.io", res, verb) {
				t.Errorf("hatchery-panel-tenant does not allow %s on %s", verb, res)
			}
		}
	}
}

func roleAllows(role rbacv1.ClusterRole, group, resource, verb string) bool {
	for _, r := range role.Rules {
		if slices.Contains(r.APIGroups, group) && slices.Contains(r.Resources, resource) && slices.Contains(r.Verbs, verb) {
			return true
		}
	}
	return false
}

func TestPanelTenantRoleReadsCrashLogs(t *testing.T) {
	raw, err := os.ReadFile("../../config/rbac/panel_tenant_role.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var role rbacv1.ClusterRole
	if err := yaml.Unmarshal(raw, &role); err != nil {
		t.Fatal(err)
	}
	if !roleAllows(role, "", "configmaps", "get") {
		t.Error("hatchery-panel-tenant must get configmaps to serve a server's crash log")
	}
}
