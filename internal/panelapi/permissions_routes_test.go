package panelapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

func gsFixture(name string) *gameserversv1alpha1.GameServer {
	return &gameserversv1alpha1.GameServer{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testOrgNS()},
		Spec: gameserversv1alpha1.GameServerSpec{
			EggRef: gameserversv1alpha1.GameServerEggRef{Name: "e"}, Storage: gameserversv1alpha1.GameServerStorage{Size: "1Gi"},
			State: gameserversv1alpha1.GameServerStateStopped,
		},
	}
}

// memberWithGrants creates a member of the fixture org holding grants and returns their token.
func memberWithGrants(t *testing.T, srv *Server, username string, grants ...paneldb.Grant) string {
	t.Helper()
	tok := newMemberToken(t, srv, username, paneldb.RoleMember)
	org, _ := srv.DB.GetOrgBySlug(context.Background(), testOrgSlug)
	u, err := srv.DB.GetUserByUsername(context.Background(), username)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.DB.SetMemberGrants(context.Background(), org.ID, u.ID, grants); err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestRoutePermissionMatrix(t *testing.T) {
	srv := newTestServer(t, gsFixture("mc"), gsFixture("other"))
	reader := memberWithGrants(t, srv, "reader", paneldb.Grant{GameServer: "mc", Permissions: []paneldb.Permission{paneldb.PermConsoleRead, paneldb.PermFilesRead}})
	admin := newMemberToken(t, srv, "adm", paneldb.RoleAdmin)

	cases := []struct {
		name, method, path, token string
		body                      any
		want                      int
	}{
		{"no grant on other: 404", http.MethodGet, orgURL("/gameservers/other"), reader, nil, http.StatusNotFound},
		{"reader sees mc", http.MethodGet, orgURL("/gameservers/mc"), reader, nil, http.StatusOK},
		{"reader cannot start", http.MethodPatch, orgURL("/gameservers/mc/state"), reader, map[string]string{"state": "Running"}, http.StatusForbidden},
		{"reader cannot restart", http.MethodPost, orgURL("/gameservers/mc/restart"), reader, nil, http.StatusForbidden},
		{"reader cannot get a console ticket", http.MethodPost, orgURL("/gameservers/mc/console-ticket"), reader, nil, http.StatusForbidden},
		{"reader cannot mkdir", http.MethodPost, orgURL("/gameservers/mc/files/mkdir"), reader, map[string]string{"path": "/x"}, http.StatusForbidden},
		{"reader cannot open sftp", http.MethodPost, orgURL("/gameservers/mc/sftp-session"), reader, nil, http.StatusForbidden},
		{"reader cannot list backups", http.MethodGet, orgURL("/gameservers/mc/backups"), reader, nil, http.StatusForbidden},
		{"reader still cannot edit settings", http.MethodPatch, orgURL("/gameservers/mc"), reader, map[string]string{"displayName": "x"}, http.StatusForbidden},
		{"admin sees other", http.MethodGet, orgURL("/gameservers/other"), admin, nil, http.StatusOK},
	}
	for _, c := range cases {
		if rec := doRequest(t, srv, c.method, c.path, c.token, c.body); rec.Code != c.want {
			t.Errorf("%s: got %d %s, want %d", c.name, rec.Code, rec.Body.String(), c.want)
		}
	}
}

func TestListIsFilteredAndWildcardCoversNewServers(t *testing.T) {
	srv := newTestServer(t, gsFixture("mc"), gsFixture("other"))
	one := memberWithGrants(t, srv, "one", paneldb.Grant{GameServer: "mc", Permissions: []paneldb.Permission{paneldb.PermConsoleRead}})
	all := memberWithGrants(t, srv, "all", paneldb.Grant{GameServer: "*", Permissions: []paneldb.Permission{paneldb.PermConsoleRead}})

	names := func(tok string) []string {
		rec := doRequest(t, srv, http.MethodGet, orgURL("/gameservers"), tok, nil)
		var list gameserversv1alpha1.GameServerList
		if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, i := range list.Items {
			out = append(out, i.Name)
		}
		return out
	}
	if got := names(one); len(got) != 1 || got[0] != "mc" {
		t.Fatalf("one: %v", got)
	}
	if err := srv.Client.Create(context.Background(), gsFixture("later")); err != nil {
		t.Fatal(err)
	}
	if got := names(all); len(got) != 3 {
		t.Fatalf("'*' must include a server created after the grant: %v", got)
	}
}

func TestGetGameServerReportsAccess(t *testing.T) {
	srv := newTestServer(t, gsFixture("mc"))
	tok := memberWithGrants(t, srv, "p", paneldb.Grant{GameServer: "mc", Permissions: []paneldb.Permission{paneldb.PermPower}})
	rec := doRequest(t, srv, http.MethodGet, orgURL("/gameservers/mc"), tok, nil)
	var got struct {
		Metadata metav1.ObjectMeta `json:"metadata"`
		Access   struct {
			Permissions []string `json:"permissions"`
		} `json:"access"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Metadata.Name != "mc" || len(got.Access.Permissions) != 1 || got.Access.Permissions[0] != "power" {
		t.Fatalf("got %+v", got)
	}
}

func TestDeletingAServerDropsItsGrants(t *testing.T) {
	srv := newTestServer(t, gsFixture("mc"))
	memberWithGrants(t, srv, "p", paneldb.Grant{GameServer: "mc", Permissions: []paneldb.Permission{paneldb.PermPower}})
	admin := newMemberToken(t, srv, "adm", paneldb.RoleAdmin)
	if rec := doRequest(t, srv, http.MethodDelete, orgURL("/gameservers/mc"), admin, nil); rec.Code != http.StatusAccepted {
		t.Fatalf("delete: %d", rec.Code)
	}
	org, _ := srv.DB.GetOrgBySlug(context.Background(), testOrgSlug)
	u, _ := srv.DB.GetUserByUsername(context.Background(), "p")
	if g, _ := srv.DB.ListMemberGrants(context.Background(), org.ID, u.ID); len(g) != 0 {
		t.Fatalf("grants on a deleted server must go: %+v", g)
	}
}

func TestConsoleHandshakeRechecksConsoleWrite(t *testing.T) {
	srv := newTestServer(t, gsFixture("mc"))
	tok := memberWithGrants(t, srv, "c", paneldb.Grant{GameServer: "mc", Permissions: []paneldb.Permission{paneldb.PermConsoleRead, paneldb.PermConsoleWrite}})
	rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers/mc/console-ticket"), tok, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("ticket: %d %s", rec.Code, rec.Body.String())
	}
	var tk struct {
		Ticket string `json:"ticket"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &tk)

	org, _ := srv.DB.GetOrgBySlug(context.Background(), testOrgSlug)
	u, _ := srv.DB.GetUserByUsername(context.Background(), "c")
	_ = srv.DB.SetMemberGrants(context.Background(), org.ID, u.ID, []paneldb.Grant{{GameServer: "mc", Permissions: []paneldb.Permission{paneldb.PermConsoleRead}}})

	rec = doRequest(t, srv, http.MethodGet, orgURL("/gameservers/mc/console?ticket="+tk.Ticket), "", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("revoked console.write between ticket and handshake: got %d, want 403", rec.Code)
	}
}
