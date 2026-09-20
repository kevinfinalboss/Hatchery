package panelapi

import (
	"net/http"
	"testing"

	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

func TestCreateGameServerIgnoresClientSuppliedNamespace(t *testing.T) {
	srv := newTestServer(t)
	admin := newMemberToken(t, srv, "orgadmin", paneldb.RoleAdmin)

	// The request struct has no namespace field, so send raw JSON that tries to smuggle one in.
	rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers"), admin, map[string]any{
		"name":      "sneaky",
		"namespace": "kube-system",
		"spec": map[string]any{
			"eggRef":  map[string]any{"name": "minecraft"},
			"storage": map[string]any{"size": "1Gi"},
		},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: got %d: %s", rec.Code, rec.Body.String())
	}

	var inOrg gameserversv1alpha1.GameServer
	if err := srv.Client.Get(t.Context(), client.ObjectKey{Namespace: testOrgNS(), Name: "sneaky"}, &inOrg); err != nil {
		t.Fatalf("the server must exist in the org's own namespace: %v", err)
	}
	var elsewhere gameserversv1alpha1.GameServer
	err := srv.Client.Get(t.Context(), client.ObjectKey{Namespace: "kube-system", Name: "sneaky"}, &elsewhere)
	if !errors.IsNotFound(err) {
		t.Fatalf("nothing may be created in the client-supplied namespace, got err=%v", err)
	}
}

func TestListGameServersOnlyShowsTheOrgsOwn(t *testing.T) {
	mine := &gameserversv1alpha1.GameServer{}
	mine.Name, mine.Namespace = "mine", testOrgNS()
	theirs := &gameserversv1alpha1.GameServer{}
	theirs.Name, theirs.Namespace = "theirs", gameserversv1alpha1.TenantNamespace("other")
	srv := newTestServer(t, mine, theirs)
	admin := adminToken(t, srv)

	rec := doRequest(t, srv, http.MethodGet, orgURL("/gameservers"), admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	body := rec.Body.String()
	if !contains(body, `"mine"`) || contains(body, `"theirs"`) {
		t.Fatalf("list must contain only the org's own servers, got %s", body)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool { return indexOf(s, sub) >= 0 })()
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
