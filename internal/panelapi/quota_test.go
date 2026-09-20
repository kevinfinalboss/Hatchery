package panelapi

import (
	"net/http"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

func tenantWithQuota(max int32, storage string) *gameserversv1alpha1.Tenant {
	return &gameserversv1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: testOrgSlug},
		Spec: gameserversv1alpha1.TenantSpec{Quota: gameserversv1alpha1.TenantQuota{
			CPU: resource.MustParse("4"), Memory: resource.MustParse("8Gi"),
			Storage: resource.MustParse(storage), MaxGameServers: max,
		}},
	}
}

func existingServer(name, size string) *gameserversv1alpha1.GameServer {
	return &gameserversv1alpha1.GameServer{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testOrgNS()},
		Spec: gameserversv1alpha1.GameServerSpec{
			EggRef:  gameserversv1alpha1.GameServerEggRef{Name: "minecraft"},
			Storage: gameserversv1alpha1.GameServerStorage{Size: size},
		},
	}
}

func createBody(name, size string) map[string]any {
	return map[string]any{"name": name, "spec": map[string]any{
		"eggRef": map[string]any{"name": "minecraft"}, "storage": map[string]any{"size": size},
	}}
}

func TestCreateGameServerStopsAtMaxGameServers(t *testing.T) {
	srv := newTestServer(t, tenantWithQuota(1, "100Gi"), existingServer("one", "1Gi"))
	admin := newMemberToken(t, srv, "adm", paneldb.RoleAdmin)

	rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers"), admin, createBody("two", "1Gi"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("got %d, want 409 for exceeding maxGameServers: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateGameServerStopsAtStorageQuota(t *testing.T) {
	srv := newTestServer(t, tenantWithQuota(10, "10Gi"), existingServer("one", "8Gi"))
	admin := newMemberToken(t, srv, "adm", paneldb.RoleAdmin)

	if rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers"), admin, createBody("big", "5Gi")); rec.Code != http.StatusConflict {
		t.Fatalf("8Gi + 5Gi over a 10Gi quota: got %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers"), admin, createBody("small", "2Gi")); rec.Code != http.StatusCreated {
		t.Fatalf("8Gi + 2Gi fits in 10Gi: got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateGameServerWithoutATenantIsNotBlockedByThePrecheck(t *testing.T) {
	// No Tenant CR in the fake cluster: the pre-check must step aside and let
	// the real ResourceQuota (the actual authority) decide.
	srv := newTestServer(t)
	admin := newMemberToken(t, srv, "adm", paneldb.RoleAdmin)
	if rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers"), admin, createBody("ok", "1Gi")); rec.Code != http.StatusCreated {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
}
