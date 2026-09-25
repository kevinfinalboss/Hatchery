package panelapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"testing"

	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

func TestSanitizeMetaDropsSecrets(t *testing.T) {
	got := sanitizeMeta(map[string]string{
		"role": "admin", "password": "hunter2", "Token": "abc", "console_ticket": "t", "file_content": "x", "path": "/data/a",
	})
	var m map[string]string
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Fatalf("metadata must be valid JSON: %v (%q)", err, got)
	}
	if len(m) != 2 || m["role"] != "admin" || m["path"] != "/data/a" {
		t.Fatalf("only role and path may survive, got %v", m)
	}
	if sanitizeMeta(nil) != "" || sanitizeMeta(map[string]string{"password": "x"}) != "" {
		t.Error("nothing left to record must give an empty string")
	}
}

func listOrgAudit(t *testing.T, srv *Server, org string) []paneldb.AuditEvent {
	t.Helper()
	evs, err := srv.DB.ListAudit(context.Background(), org, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	return evs
}

func TestMutatingActionsAreAuditedAndReadsAreNot(t *testing.T) {
	srv := newTestServer(t)
	admin := newMemberToken(t, srv, "opadmin", paneldb.RoleAdmin)

	doRequest(t, srv, http.MethodGet, orgURL("/gameservers"), admin, nil)          // read
	doRequest(t, srv, http.MethodDelete, orgURL("/gameservers/ghost"), admin, nil) // missing server: fails

	evs := listOrgAudit(t, srv, testOrgSlug)
	if len(evs) != 1 {
		t.Fatalf("expected exactly one event (the DELETE), got %+v", evs)
	}
	e := evs[0]
	if e.Action != "gameserver.delete" || e.TargetName != "ghost" || e.ActorUsername != "opadmin" || e.Outcome != "failed" {
		t.Errorf("unexpected event %+v", e)
	}
	if e.ActorUserID == nil {
		t.Error("the actor's user id must be recorded")
	}
}

func TestCreateGameServerIsAuditedWithItsName(t *testing.T) {
	srv := newTestServer(t)
	admin := newMemberToken(t, srv, "opadmin", paneldb.RoleAdmin)
	rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers"), admin, map[string]any{
		"name": "audited-one",
		"spec": map[string]any{"eggRef": map[string]any{"name": "minecraft"}, "storage": map[string]any{"size": "1Gi"}},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	evs := listOrgAudit(t, srv, testOrgSlug)
	if len(evs) != 1 || evs[0].Action != "gameserver.create" || evs[0].TargetName != "audited-one" || evs[0].Outcome != "success" {
		t.Fatalf("got %+v", evs)
	}
}

func TestDeniedRequestsAreNotRecordedInsideTheOrg(t *testing.T) {
	srv := newTestServer(t)
	member := newMemberToken(t, srv, "m", paneldb.RoleMember)
	doRequest(t, srv, http.MethodDelete, orgURL("/gameservers/x"), member, nil)
	if evs := listOrgAudit(t, srv, testOrgSlug); len(evs) != 0 {
		t.Fatalf("expected no events, got %+v", evs)
	}
}

func TestAuditFailureDoesNotBreakTheRequest(t *testing.T) {
	srv := newTestServer(t)
	srv.recordAuditFn = func(context.Context, paneldb.AuditEvent) error { return errors.New("disk full") }
	admin := newMemberToken(t, srv, "opadmin", paneldb.RoleAdmin)

	rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers"), admin, map[string]any{
		"name": "still-created",
		"spec": map[string]any{"eggRef": map[string]any{"name": "minecraft"}, "storage": map[string]any{"size": "1Gi"}},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("an audit write failure must not fail the action, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestLoginEventsAreRecordedWithoutThePassword(t *testing.T) {
	srv := newTestServer(t)
	if _, err := srv.DB.CreateUser(t.Context(), "u", "u@example.com", "correct", false); err != nil {
		t.Fatal(err)
	}
	login(t, srv, "203.0.113.5:1", "", "u", "hunter2-wrong")
	login(t, srv, "203.0.113.5:1", "", "u", "correct")

	evs, err := srv.DB.ListAudit(t.Context(), "", 10, 0)
	if err != nil || len(evs) != 2 {
		t.Fatalf("platform events = %+v, %v; want login.failure and login.success", evs, err)
	}
	if evs[1].Action != "login.failure" || evs[0].Action != "login.success" {
		t.Errorf("unexpected order/actions: %+v", evs)
	}
	raw, _ := json.Marshal(evs)
	for _, secret := range []string{"hunter2-wrong", "correct"} {
		if contains(string(raw), secret) {
			t.Fatalf("a password leaked into the audit log: %s", raw)
		}
	}
	if evs[0].ActorUserID == nil || evs[1].ActorUserID != nil {
		t.Error("success has an actor id, a failed login (unknown identity) does not")
	}
}

func TestConsoleTicketIsAuditedButTheTicketValueIsNot(t *testing.T) {
	srv := newTestServer(t)
	member := memberWithGrants(t, srv, "m", paneldb.Grant{GameServer: paneldb.AllServers, Permissions: paneldb.AllPermissions})
	ticket := issueTicket(t, srv, member, "mc")

	evs := listOrgAudit(t, srv, testOrgSlug)
	if len(evs) != 1 || evs[0].Action != "gameserver.console-ticket" {
		t.Fatalf("got %+v", evs)
	}
	raw, _ := json.Marshal(evs)
	if contains(string(raw), ticket) {
		t.Fatal("the ticket value must never be written to the audit log")
	}
}

type auditPage struct {
	Events     []paneldb.AuditEvent `json:"events"`
	NextBefore *int64               `json:"nextBefore"`
}

func getAuditPage(t *testing.T, srv *Server, path, token string, wantCode int) auditPage {
	t.Helper()
	rec := doRequest(t, srv, http.MethodGet, path, token, nil)
	if rec.Code != wantCode {
		t.Fatalf("GET %s: got %d, want %d: %s", path, rec.Code, wantCode, rec.Body.String())
	}
	var p auditPage
	if wantCode == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestAuditListingPermissionsAndPaging(t *testing.T) {
	srv := newTestServer(t)
	adminTok := newMemberToken(t, srv, "orgadmin", paneldb.RoleAdmin)
	memberTok := newMemberToken(t, srv, "plainmember", paneldb.RoleMember)
	outsider := newUserToken(t, srv, "outsider", false)
	for i := 0; i < 3; i++ {
		doRequest(t, srv, http.MethodDelete, orgURL("/gameservers/ghost"), adminTok, nil)
	}

	getAuditPage(t, srv, orgURL("/audit"), memberTok, http.StatusForbidden)
	getAuditPage(t, srv, orgURL("/audit"), outsider, http.StatusNotFound)

	page := getAuditPage(t, srv, orgURL("/audit?limit=2"), adminTok, http.StatusOK)
	if len(page.Events) != 2 || page.NextBefore == nil {
		t.Fatalf("limit=2 of 3 events: got %d events, nextBefore=%v", len(page.Events), page.NextBefore)
	}
	next := getAuditPage(t, srv, orgURL("/audit?limit=2&before="+itoa(*page.NextBefore)), adminTok, http.StatusOK)
	if len(next.Events) != 1 || next.NextBefore != nil {
		t.Fatalf("second page: got %d events, nextBefore=%v; want 1 and null", len(next.Events), next.NextBefore)
	}
}

func TestPlatformAuditIsPlatformAdminOnly(t *testing.T) {
	srv := newTestServer(t)
	owner := newMemberToken(t, srv, "orgowner", paneldb.RoleOwner)
	getAuditPage(t, srv, "/api/v1/audit", owner, http.StatusForbidden)
	getAuditPage(t, srv, "/api/v1/audit", adminToken(t, srv), http.StatusOK)
}

func TestOrgAuditNeverShowsAnotherOrgsEvents(t *testing.T) {
	srv := newTestServer(t)
	other, _ := srv.DB.CreateUser(t.Context(), "elsewhere", "elsewhere@example.com", "password", false)
	if _, err := srv.DB.CreateOrg(t.Context(), "otherorg", "Other", other.ID); err != nil {
		t.Fatal(err)
	}
	if err := srv.DB.RecordAudit(t.Context(), paneldb.AuditEvent{OrgSlug: "otherorg", ActorUsername: "elsewhere", Action: "secret.action", Outcome: "success"}); err != nil {
		t.Fatal(err)
	}
	adminTok := newMemberToken(t, srv, "orgadmin", paneldb.RoleAdmin)
	page := getAuditPage(t, srv, orgURL("/audit"), adminTok, http.StatusOK)
	for _, e := range page.Events {
		if e.OrgSlug != testOrgSlug {
			t.Fatalf("leaked event from another org: %+v", e)
		}
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
