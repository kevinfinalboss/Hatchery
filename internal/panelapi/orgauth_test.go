package panelapi

import (
	"net/http"
	"testing"

	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

// The route is registered in Task 3; until then these tests exercise the
// middleware through the real routes table, so they are written against the
// final URLs and only pass once Task 3 is done.
func TestOrgRoleMatrixOnGameServerList(t *testing.T) {
	srv := newTestServer(t)
	admin := adminToken(t, srv) // platform admin
	outsider := newUserToken(t, srv, "outsider", false)
	member := newMemberToken(t, srv, "m", paneldb.RoleMember)

	cases := []struct {
		name  string
		token string
		path  string
		want  int
	}{
		{"no token", "", orgURL("/gameservers"), http.StatusUnauthorized},
		{"non-member gets 404, not 403", outsider, orgURL("/gameservers"), http.StatusNotFound},
		{"member can list", member, orgURL("/gameservers"), http.StatusOK},
		{"platform admin can list", admin, orgURL("/gameservers"), http.StatusOK},
		{"unknown org is 404 even for platform admin", admin, "/api/v1/orgs/nope/gameservers", http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if rec := doRequest(t, srv, http.MethodGet, c.path, c.token, nil); rec.Code != c.want {
				t.Fatalf("got %d, want %d: %s", rec.Code, c.want, rec.Body.String())
			}
		})
	}
}

func TestMemberCannotCreateOrDeleteGameServers(t *testing.T) {
	srv := newTestServer(t)
	member := newMemberToken(t, srv, "m", paneldb.RoleMember)
	adminRole := newMemberToken(t, srv, "a", paneldb.RoleAdmin)

	if rec := doRequest(t, srv, http.MethodDelete, orgURL("/gameservers/x"), member, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("member DELETE: got %d, want 403", rec.Code)
	}
	// An org admin passes the role check (the server does not exist, so 404 from Kubernetes).
	if rec := doRequest(t, srv, http.MethodDelete, orgURL("/gameservers/x"), adminRole, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("admin DELETE of a missing server: got %d, want 404", rec.Code)
	}
}

func TestOldGlobalGameServerRoutesAreGone(t *testing.T) {
	srv := newTestServer(t)
	admin := adminToken(t, srv)
	if rec := doRequest(t, srv, http.MethodGet, "/api/v1/gameservers", admin, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("the namespace-in-URL API must not exist any more, got %d", rec.Code)
	}
}
