package panelapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

func issueTicket(t *testing.T, srv *Server, token, gs string) string {
	t.Helper()
	rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers/"+gs+"/console-ticket"), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("console-ticket: got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Ticket           string `json:"ticket"`
		ExpiresInSeconds int    `json:"expiresInSeconds"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.Ticket == "" || resp.ExpiresInSeconds != 30 {
		t.Fatalf("bad ticket response %s (%v)", rec.Body.String(), err)
	}
	return resp.Ticket
}

// handshake drives requireConsoleTicket through the real route pattern, with a
// sentinel in place of handleConsole (which needs a live cluster to attach to).
func handshake(t *testing.T, srv *Server, path string, sawNamespace *string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/orgs/{org}/gameservers/{name}/console", srv.requireConsoleTicket(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*sawNamespace = r.PathValue("namespace")
			if orgAccessFromContext(r.Context()) == nil || userFromContext(r.Context()) == nil {
				t.Error("the handshake must put user and org access in the context")
			}
			w.WriteHeader(http.StatusSwitchingProtocols)
		})))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestConsoleTicketRequiresMembership(t *testing.T) {
	srv := newTestServer(t)
	outsider := newUserToken(t, srv, "outsider", false)
	if rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers/mc/console-ticket"), outsider, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("a non-member must not get a ticket: got %d", rec.Code)
	}
	if rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers/mc/console-ticket"), "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("an unauthenticated caller must not get a ticket: got %d", rec.Code)
	}
}

func TestConsoleHandshakeAcceptsAValidTicketExactlyOnce(t *testing.T) {
	srv := newTestServer(t)
	member := newMemberToken(t, srv, "m", paneldb.RoleMember)
	ticket := issueTicket(t, srv, member, "mc")

	var ns string
	path := orgURL("/gameservers/mc/console") + "?ticket=" + ticket
	if rec := handshake(t, srv, path, &ns); rec.Code != http.StatusSwitchingProtocols {
		t.Fatalf("first use: got %d: %s", rec.Code, rec.Body.String())
	}
	if ns != gameserversv1alpha1.TenantNamespace(testOrgSlug) {
		t.Errorf("namespace path value = %q, want the org's derived namespace", ns)
	}
	if rec := handshake(t, srv, path, &ns); rec.Code != http.StatusUnauthorized {
		t.Fatalf("second use of the same ticket: got %d, want 401", rec.Code)
	}
}

func TestConsoleHandshakeRejectsBadInputs(t *testing.T) {
	srv := newTestServer(t)
	member := newMemberToken(t, srv, "m", paneldb.RoleMember)
	var ns string

	if rec := handshake(t, srv, orgURL("/gameservers/mc/console"), &ns); rec.Code != http.StatusUnauthorized {
		t.Errorf("no ticket: got %d", rec.Code)
	}
	if rec := handshake(t, srv, orgURL("/gameservers/mc/console")+"?ticket=garbage", &ns); rec.Code != http.StatusUnauthorized {
		t.Errorf("unknown ticket: got %d", rec.Code)
	}

	// A session token in the URL is no longer accepted anywhere.
	if rec := handshake(t, srv, orgURL("/gameservers/mc/console")+"?token="+member, &ns); rec.Code != http.StatusUnauthorized {
		t.Errorf("?token= must be dead: got %d", rec.Code)
	}

	// A ticket is bound to one server: it must not open another one's console.
	tk := issueTicket(t, srv, member, "mc")
	if rec := handshake(t, srv, orgURL("/gameservers/other-server/console")+"?ticket="+tk, &ns); rec.Code != http.StatusUnauthorized {
		t.Errorf("ticket for mc used on other-server: got %d, want 401", rec.Code)
	}
	// ...and it was consumed by that failed attempt, so it cannot be retried on the right one.
	if rec := handshake(t, srv, orgURL("/gameservers/mc/console")+"?ticket="+tk, &ns); rec.Code != http.StatusUnauthorized {
		t.Errorf("a ticket burned by a mismatched attempt must stay burned: got %d", rec.Code)
	}
}

func TestConsoleHandshakeRevalidatesMembership(t *testing.T) {
	srv := newTestServer(t)
	member := newMemberToken(t, srv, "leaver", paneldb.RoleMember)
	ticket := issueTicket(t, srv, member, "mc")

	// The user is removed from the org in the seconds between asking and using.
	org, _ := srv.DB.GetOrgBySlug(t.Context(), testOrgSlug)
	u, _ := srv.DB.GetUserByUsername(t.Context(), "leaver")
	if err := srv.DB.RemoveMember(t.Context(), org.ID, u.ID); err != nil {
		t.Fatal(err)
	}

	var ns string
	rec := handshake(t, srv, orgURL("/gameservers/mc/console")+"?ticket="+ticket, &ns)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("a removed member's ticket must not open the console: got %d, want 404", rec.Code)
	}
}
