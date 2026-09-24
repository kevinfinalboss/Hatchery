package panelapi

import (
	"net/http"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

func getServer(t *testing.T, srv *Server, name string) gameserversv1alpha1.GameServer {
	t.Helper()
	var gs gameserversv1alpha1.GameServer
	if err := srv.Client.Get(t.Context(), client.ObjectKey{Namespace: testOrgNS(), Name: name}, &gs); err != nil {
		t.Fatal(err)
	}
	return gs
}

func TestOnlyAPlatformAdminCanSuspendAndUnsuspend(t *testing.T) {
	running := editableServer()
	running.Spec.State = gameserversv1alpha1.GameServerStateRunning
	srv := newTestServer(t, customizeEgg(), running)
	platform := adminToken(t, srv)
	owner := newMemberToken(t, srv, "own", paneldb.RoleOwner)
	url := orgURL("/gameservers/edit-me/")

	if rec := doRequest(t, srv, http.MethodPost, url+"suspend", owner, map[string]string{"reason": "x"}); rec.Code != http.StatusForbidden {
		t.Fatalf("org owner suspending: got %d, want 403", rec.Code)
	}
	if rec := doRequest(t, srv, http.MethodPost, url+"suspend", platform, map[string]string{"reason": ""}); rec.Code != http.StatusBadRequest {
		t.Fatalf("a reason is required: got %d, want 400", rec.Code)
	}
	if rec := doRequest(t, srv, http.MethodPost, url+"suspend", platform, map[string]string{"reason": "unpaid invoice"}); rec.Code != http.StatusAccepted {
		t.Fatalf("suspend: got %d: %s", rec.Code, rec.Body.String())
	}
	gs := getServer(t, srv, "edit-me")
	if !gs.Spec.Suspended || gs.Spec.SuspendReason != "unpaid invoice" || gs.Spec.State != gameserversv1alpha1.GameServerStateStopped {
		t.Fatalf("suspending must also stop the server: %+v", gs.Spec)
	}

	if rec := doRequest(t, srv, http.MethodPost, url+"unsuspend", owner, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("org owner unsuspending: got %d, want 403", rec.Code)
	}
	if rec := doRequest(t, srv, http.MethodPost, url+"unsuspend", platform, nil); rec.Code != http.StatusAccepted {
		t.Fatalf("unsuspend: got %d: %s", rec.Code, rec.Body.String())
	}
	gs = getServer(t, srv, "edit-me")
	if gs.Spec.Suspended || gs.Spec.SuspendReason != "" || gs.Spec.State != gameserversv1alpha1.GameServerStateStopped {
		t.Fatalf("unsuspending clears the flag but does not start the server: %+v", gs.Spec)
	}
}

func TestASuspendedServerIsLockedForTheOrg(t *testing.T) {
	gs := editableServer()
	gs.Spec.Suspended = true
	gs.Spec.SuspendReason = "abuse report"
	srv := newTestServer(t, customizeEgg(), gs)
	platform := adminToken(t, srv)
	owner := newMemberToken(t, srv, "own", paneldb.RoleOwner)
	member := memberWithGrants(t, srv, "mem", paneldb.Grant{GameServer: paneldb.AllServers, Permissions: paneldb.AllPermissions})
	base := orgURL("/gameservers/edit-me")

	locked := []struct {
		method, path string
		body         any
	}{
		{http.MethodPatch, "", map[string]any{"displayName": "x"}},
		{http.MethodPatch, "/state", map[string]string{"state": "Running"}},
		{http.MethodPost, "/restart", nil},
		{http.MethodPost, "/reinstall", nil},
		{http.MethodDelete, "", nil},
		{http.MethodGet, "/files?path=/", nil},
		{http.MethodGet, "/logs", nil},
		{http.MethodGet, "/metrics", nil},
		{http.MethodPost, "/sftp-session", nil},
		{http.MethodPost, "/console-ticket", nil},
	}
	for _, tok := range []string{owner, member} {
		for _, c := range locked {
			rec := doRequest(t, srv, c.method, base+c.path, tok, c.body)
			if rec.Code == http.StatusForbidden { // the route needs a higher role than this token has
				continue
			}
			if rec.Code != http.StatusLocked {
				t.Fatalf("%s %s: got %d, want 423: %s", c.method, c.path, rec.Code, rec.Body.String())
			}
			if !contains(rec.Body.String(), "abuse report") {
				t.Fatalf("%s %s: the reason must reach the org: %s", c.method, c.path, rec.Body.String())
			}
		}
	}

	rec := doRequest(t, srv, http.MethodGet, base, member, nil)
	if rec.Code != http.StatusOK || !contains(rec.Body.String(), "abuse report") {
		t.Fatalf("the org can still see the server and why: got %d %s", rec.Code, rec.Body.String())
	}

	// The platform admin is not locked out: they need to investigate and clean up.
	if rec := doRequest(t, srv, http.MethodGet, base+"/metrics", platform, nil); rec.Code == http.StatusLocked {
		t.Fatalf("a platform admin must not be locked out")
	}
}

func TestOrgAdminsCannotTouchTheSuspensionFieldsThroughPatch(t *testing.T) {
	srv := newTestServer(t, customizeEgg(), editableServer())
	admin := newMemberToken(t, srv, "adm", paneldb.RoleAdmin)
	rec := doRequest(t, srv, http.MethodPatch, orgURL("/gameservers/edit-me"), admin, map[string]any{
		"displayName": "Ok", "suspended": true, "suspendReason": "self-inflicted",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	if gs := getServer(t, srv, "edit-me"); gs.Spec.Suspended || gs.Spec.SuspendReason != "" {
		t.Fatalf("the PATCH must ignore suspension fields: %+v", gs.Spec)
	}
}

func TestStartCommandCanBeSetValidatedAndReset(t *testing.T) {
	srv := newTestServer(t, customizeEgg(), editableServer())
	admin := newMemberToken(t, srv, "adm", paneldb.RoleAdmin)
	member := newMemberToken(t, srv, "mem", paneldb.RoleMember)
	url := orgURL("/gameservers/edit-me")

	if rec := doRequest(t, srv, http.MethodPatch, url, member, map[string]any{"startCommand": "x"}); rec.Code != http.StatusForbidden {
		t.Fatalf("member: got %d, want 403", rec.Code)
	}
	if rec := doRequest(t, srv, http.MethodPatch, url, admin, map[string]any{"startCommand": "run {{NOPE}}"}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("undeclared placeholder: got %d, want 422: %s", rec.Code, rec.Body.String())
	}
	if rec := doRequest(t, srv, http.MethodPatch, url, admin, map[string]any{"startCommand": "run\nrm"}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("multi-line: got %d, want 422", rec.Code)
	}
	if rec := doRequest(t, srv, http.MethodPatch, url, admin, map[string]any{"startCommand": "run --motd {{MOTD}}"}); rec.Code != http.StatusOK {
		t.Fatalf("valid: got %d: %s", rec.Code, rec.Body.String())
	}
	if got := getServer(t, srv, "edit-me").Spec.StartCommand; got != "run --motd {{MOTD}}" {
		t.Fatalf("stored %q", got)
	}
	if rec := doRequest(t, srv, http.MethodPatch, url, admin, map[string]any{"startCommand": ""}); rec.Code != http.StatusOK {
		t.Fatalf("reset: got %d: %s", rec.Code, rec.Body.String())
	}
	if got := getServer(t, srv, "edit-me").Spec.StartCommand; got != "" {
		t.Fatalf("an empty string restores the Egg's command, got %q", got)
	}
}
