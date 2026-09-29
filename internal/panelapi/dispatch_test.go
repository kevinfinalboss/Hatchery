package panelapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

func dispatchFixture(t *testing.T) *Server {
	return newTestServer(t, fixtureObjects()...)
}

// fixtureObjects is a stopped server "mc" whose Pod still runs the game container.
func fixtureObjects() []client.Object {
	gs := &v1.GameServer{
		ObjectMeta: metav1.ObjectMeta{Name: "mc", Namespace: testOrgNS()},
		Spec:       v1.GameServerSpec{EggRef: v1.GameServerEggRef{Name: "egg"}, State: v1.GameServerStateStopped},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "mc", Namespace: testOrgNS()},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{
			{Name: "server", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
		}},
	}
	return []client.Object{gs, pod}
}

func userNamed(t *testing.T, srv *Server, name string) *paneldb.User {
	u, err := srv.DB.GetUserByUsername(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestDispatchActsAsTheLinkedUser(t *testing.T) {
	srv := dispatchFixture(t)
	newMemberToken(t, srv, "adm", paneldb.RoleAdmin)
	newMemberToken(t, srv, "mem", paneldb.RoleMember)
	ctx := context.Background()

	// A member with no grants gets exactly what the panel would give them.
	if code, _ := srv.dispatch(ctx, userNamed(t, srv, "mem"), "discord", http.MethodPatch, orgURL("/gameservers/mc/state"), map[string]string{"state": "Running"}); code != http.StatusNotFound {
		t.Fatalf("member without grant = %d, want 404", code)
	}
	code, body := srv.dispatch(ctx, userNamed(t, srv, "adm"), "discord", http.MethodPatch, orgURL("/gameservers/mc/state"), map[string]string{"state": "Running"})
	if code != http.StatusOK {
		t.Fatalf("admin = %d %s", code, body)
	}
	var gs v1.GameServer
	_ = srv.Client.Get(ctx, types.NamespacedName{Namespace: testOrgNS(), Name: "mc"}, &gs)
	if gs.Spec.State != v1.GameServerStateRunning {
		t.Fatal("state not changed")
	}
	events, _ := srv.DB.ListAudit(ctx, testOrgSlug, 20, 0)
	found := false
	for _, e := range events {
		if e.Action == "gameserver.state" && e.ActorUsername == "adm" && strings.Contains(e.Metadata, `"via":"discord"`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no audit event with via=discord: %+v", events)
	}

	// Without the internal context key, a request needs a real session.
	rec := doRequest(t, srv, http.MethodPatch, orgURL("/gameservers/mc/state"), "", map[string]string{"state": "Stopped"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("external request without token = %d", rec.Code)
	}
}

func TestConsoleCommandRoute(t *testing.T) {
	srv := dispatchFixture(t)
	admin := newMemberToken(t, srv, "adm", paneldb.RoleAdmin)
	var ran []string
	srv.execFn = func(_ context.Context, ns, pod, container string, cmd []string) (string, error) {
		ran = append(ran, ns+"/"+pod+"/"+container+":"+strings.Join(cmd, " "))
		return "", nil
	}
	send := func(token, command string) int {
		return doRequest(t, srv, http.MethodPost, orgURL("/gameservers/mc/command"), token, map[string]string{"command": command}).Code
	}
	if c := send(admin, "say hi"); c != http.StatusAccepted {
		t.Fatalf("command = %d", c)
	}
	if len(ran) != 1 || !strings.HasSuffix(ran[0], "/server:sh -c printf '%s\\n' \"$1\" > /proc/1/fd/0 sh say hi") {
		t.Fatalf("exec = %v", ran)
	}
	if c := send(admin, "say a\nstop"); c != http.StatusUnprocessableEntity {
		t.Fatalf("multi-line = %d, want 422", c)
	}
	if c := send(admin, ""); c != http.StatusUnprocessableEntity {
		t.Fatalf("empty = %d, want 422", c)
	}
	member := newMemberToken(t, srv, "mem", paneldb.RoleMember)
	org, _ := srv.DB.GetOrgBySlug(context.Background(), testOrgSlug)
	mem := userNamed(t, srv, "mem")
	_ = srv.DB.SetMemberGrants(context.Background(), org.ID, mem.ID, []paneldb.Grant{{GameServer: "mc", Permissions: []paneldb.Permission{paneldb.PermPower}}})
	if c := send(member, "say hi"); c != http.StatusForbidden {
		t.Fatalf("member without console.write = %d, want 403", c)
	}
	// No running game container: nothing to type into.
	var pod corev1.Pod
	_ = srv.Client.Get(context.Background(), types.NamespacedName{Namespace: testOrgNS(), Name: "mc"}, &pod)
	_ = srv.Client.Delete(context.Background(), &pod)
	if c := send(admin, "say hi"); c != http.StatusConflict {
		t.Fatalf("no pod = %d, want 409", c)
	}
}
