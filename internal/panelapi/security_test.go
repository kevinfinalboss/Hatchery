package panelapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

func errCode(t *testing.T, body []byte) string {
	t.Helper()
	var e struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(body, &e)
	return e.Code
}

func requireOrg2FA(t *testing.T, srv *Server) {
	t.Helper()
	org, _ := srv.DB.GetOrgBySlug(context.Background(), testOrgSlug)
	if err := srv.DB.SetOrgRequire2FA(context.Background(), org.ID, true); err != nil {
		t.Fatal(err)
	}
}

func TestOrgRequiring2FABlocksMembersWithoutIt(t *testing.T) {
	srv := newTestServer(t, fixtureObjects()...)
	now := pinnedClock(srv)
	mem := newMemberToken(t, srv, "mem", paneldb.RoleMember)
	org, _ := srv.DB.GetOrgBySlug(context.Background(), testOrgSlug)
	_ = srv.DB.SetMemberGrants(context.Background(), org.ID, userNamed(t, srv, "mem").ID,
		[]paneldb.Grant{{GameServer: "*", Permissions: []paneldb.Permission{paneldb.PermPower}}})
	outsider := newUserToken(t, srv, "outsider", false)
	requireOrg2FA(t, srv)

	rec := doRequest(t, srv, http.MethodGet, orgURL("/gameservers"), mem, nil)
	if rec.Code != http.StatusForbidden || errCode(t, rec.Body.Bytes()) != "two_factor_required" {
		t.Fatalf("member without 2FA = %d %s", rec.Code, rec.Body)
	}
	if rec := doRequest(t, srv, http.MethodGet, orgURL("/gameservers"), outsider, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("non-member = %d, want 404", rec.Code)
	}

	// The org list and the security page still answer, so the UI can send them to set it up.
	rec = doRequest(t, srv, http.MethodGet, "/api/v1/orgs", mem, nil)
	if !strings.Contains(rec.Body.String(), `"twoFactorRequired":true`) {
		t.Fatalf("org list = %s", rec.Body)
	}
	rec = doRequest(t, srv, http.MethodGet, orgURL("/security"), mem, nil)
	var sec struct {
		Require2FA        bool `json:"require2fa"`
		Blocked           bool `json:"blocked"`
		MembersWithout2FA *int `json:"membersWithout2fa"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &sec)
	if rec.Code != http.StatusOK || !sec.Require2FA || !sec.Blocked || sec.MembersWithout2FA != nil {
		t.Fatalf("security for a blocked member = %d %s", rec.Code, rec.Body)
	}

	_, _ = enable2FA(t, srv, mem, *now)
	if rec := doRequest(t, srv, http.MethodGet, orgURL("/gameservers"), mem, nil); rec.Code != http.StatusOK {
		t.Fatalf("member with 2FA = %d %s", rec.Code, rec.Body)
	}
	rec = doRequest(t, srv, http.MethodGet, "/api/v1/orgs", mem, nil)
	if !strings.Contains(rec.Body.String(), `"twoFactorRequired":false`) {
		t.Fatalf("org list after enabling = %s", rec.Body)
	}
}

func TestOnlyAdminsWith2FAChangeTheRequirement(t *testing.T) {
	srv := newTestServer(t)
	now := pinnedClock(srv)
	adm := newMemberToken(t, srv, "adm", paneldb.RoleAdmin)
	put := func(on bool) *httpResult {
		rec := doRequest(t, srv, http.MethodPut, orgURL("/security"), adm, map[string]bool{"require2fa": on})
		return &httpResult{rec.Code, rec.Body.Bytes()}
	}
	if r := put(true); r.code != http.StatusConflict {
		t.Fatalf("admin without 2FA turning it on = %d", r.code)
	}
	_, _ = enable2FA(t, srv, adm, *now)
	if r := put(true); r.code != http.StatusOK {
		t.Fatalf("admin with 2FA turning it on = %d %s", r.code, r.body)
	}
	rec := doRequest(t, srv, http.MethodGet, orgURL("/security"), adm, nil)
	if !strings.Contains(rec.Body.String(), `"membersWithout2fa":1`) { // the fixture owner
		t.Fatalf("security = %s", rec.Body)
	}
	// Someone with an admin's password but without 2FA cannot switch the requirement off.
	adm2 := newMemberToken(t, srv, "adm2", paneldb.RoleAdmin)
	rec = doRequest(t, srv, http.MethodPut, orgURL("/security"), adm2, map[string]bool{"require2fa": false})
	if rec.Code != http.StatusForbidden || errCode(t, rec.Body.Bytes()) != "two_factor_required" {
		t.Fatalf("admin without 2FA turning it off = %d %s", rec.Code, rec.Body)
	}
	if r := put(false); r.code != http.StatusOK {
		t.Fatalf("turning it off = %d", r.code)
	}
	// With the requirement off, turning it off again without 2FA is still refused (409).
	rec = doRequest(t, srv, http.MethodPut, orgURL("/security"), adm2, map[string]bool{"require2fa": false})
	if rec.Code != http.StatusConflict {
		t.Fatalf("admin without 2FA, requirement off = %d", rec.Code)
	}
	events, _ := srv.DB.ListAudit(context.Background(), testOrgSlug, 50, 0)
	n := 0
	for _, e := range events {
		if e.Action == "org.security.update" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("org.security.update events = %d, want 2", n)
	}
}

type httpResult struct {
	code int
	body []byte
}

func TestPlatformRequiring2FAForAdmins(t *testing.T) {
	srv := newTestServer(t)
	now := pinnedClock(srv)
	root := newUserToken(t, srv, "root", true)
	other := newUserToken(t, srv, "other-admin", true)

	if rec := doRequest(t, srv, http.MethodPut, "/api/v1/platform/security", root, map[string]bool{"requireAdminTwoFactor": true}); rec.Code != http.StatusConflict {
		t.Fatalf("admin without 2FA turning it on = %d", rec.Code)
	}
	_, _ = enable2FA(t, srv, root, *now)
	*now = now.Add(time.Minute)
	if rec := doRequest(t, srv, http.MethodPut, "/api/v1/platform/security", root, map[string]bool{"requireAdminTwoFactor": true}); rec.Code != http.StatusOK {
		t.Fatalf("turning it on = %d %s", rec.Code, rec.Body)
	}

	rec := doRequest(t, srv, http.MethodGet, "/api/v1/users", other, nil)
	if rec.Code != http.StatusForbidden || errCode(t, rec.Body.Bytes()) != "two_factor_required" {
		t.Fatalf("admin without 2FA on an admin route = %d %s", rec.Code, rec.Body)
	}
	rec = doRequest(t, srv, http.MethodGet, orgURL("/members"), other, nil)
	if rec.Code != http.StatusForbidden || errCode(t, rec.Body.Bytes()) != "two_factor_required" {
		t.Fatalf("admin without 2FA in an org = %d %s", rec.Code, rec.Body)
	}
	rec = doRequest(t, srv, http.MethodGet, "/api/v1/platform/security", other, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"blocked":true`) {
		t.Fatalf("platform security for a blocked admin = %d %s", rec.Code, rec.Body)
	}
	if rec := doRequest(t, srv, http.MethodGet, "/api/v1/users", root, nil); rec.Code != http.StatusOK {
		t.Fatalf("admin with 2FA = %d", rec.Code)
	}
	// Plain users are not affected by the platform rule.
	user := newUserToken(t, srv, "plain", false)
	if rec := doRequest(t, srv, http.MethodGet, "/api/v1/auth/me", user, nil); rec.Code != http.StatusOK {
		t.Fatalf("plain user = %d", rec.Code)
	}
}

func TestDiscordExplainsTheTwoFactorRequirement(t *testing.T) {
	f := interactionSetup(t)
	requireOrg2FA(t, f.srv)
	r := f.run(t, "g1", "42", "start", map[string]string{"server": "mc"})
	if !strings.Contains(r.Data.Content, "2FA") || !strings.Contains(r.Data.Content, "/account") {
		t.Fatalf("start in an org requiring 2FA = %q", r.Data.Content)
	}
}
