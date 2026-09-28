package panelapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	v1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

func TestPatchExtraPorts(t *testing.T) {
	egg := &v1.Egg{
		ObjectMeta: metav1.ObjectMeta{Name: "mc-egg", Namespace: testOrgNS()},
		Spec: v1.EggSpec{
			Images: []v1.EggImage{{Name: "default", Image: "example.com/g:1"}}, StartCommand: "run",
			Ports: []v1.EggPort{{Name: "game", ContainerPort: 25565}},
		},
	}
	gs := &v1.GameServer{
		ObjectMeta: metav1.ObjectMeta{Name: "mc", Namespace: testOrgNS()},
		Spec:       v1.GameServerSpec{EggRef: v1.GameServerEggRef{Name: "mc-egg"}},
	}
	srv := newTestServer(t, egg, gs)
	admin := newMemberToken(t, srv, "adm", paneldb.RoleAdmin)
	patch := func(token string, body map[string]any) int {
		return doRequest(t, srv, http.MethodPatch, orgURL("/gameservers/mc"), token, body).Code
	}
	extras := func() []v1.GameServerExtraPort {
		var got v1.GameServer
		if err := srv.Client.Get(context.Background(), types.NamespacedName{Namespace: testOrgNS(), Name: "mc"}, &got); err != nil {
			t.Fatal(err)
		}
		return got.Spec.ExtraPorts
	}

	dynmap := map[string]any{"name": "dynmap", "containerPort": 8123, "protocol": "TCP"}
	if c := patch(admin, map[string]any{"extraPorts": []any{dynmap}}); c != http.StatusOK {
		t.Fatalf("valid extra = %d", c)
	}
	if got := extras(); len(got) != 1 || got[0].ContainerPort != 8123 || got[0].Protocol != corev1.ProtocolTCP {
		t.Fatalf("stored = %+v", got)
	}
	rec := doRequest(t, srv, http.MethodPatch, orgURL("/gameservers/mc"), admin,
		map[string]any{"extraPorts": []any{map[string]any{"name": "clash", "containerPort": 25565, "protocol": "TCP"}}})
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "game") {
		t.Fatalf("collision = %d %s", rec.Code, rec.Body)
	}
	if c := patch(admin, map[string]any{"displayName": "Survival"}); c != http.StatusOK || len(extras()) != 1 {
		t.Fatalf("omitting extraPorts must keep them: code %d, extras %+v", c, extras())
	}
	if c := patch(admin, map[string]any{"extraPorts": []any{}}); c != http.StatusOK || len(extras()) != 0 {
		t.Fatalf("[] must remove them: code %d, extras %+v", c, extras())
	}
	member := newMemberToken(t, srv, "mem", paneldb.RoleMember)
	if c := patch(member, map[string]any{"extraPorts": []any{dynmap}}); c == http.StatusOK {
		t.Fatal("a member must not change extra ports")
	}
}
