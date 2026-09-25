package panelapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/kevinfinalboss/Hatchery/internal/mailer"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

func lastLinkToken(t *testing.T, mails *mailer.Recorder) string {
	t.Helper()
	sent := mails.Sent()
	if len(sent) == 0 {
		t.Fatal("no e-mail sent")
	}
	return tokenFromLink(t, sent[len(sent)-1].Text)
}

func TestInviteRules(t *testing.T) {
	srv, mails := newTestServerWithMail(t)
	adminTok := newMemberToken(t, srv, "admin1", paneldb.RoleAdmin)
	memberTok := newMemberToken(t, srv, "member1", paneldb.RoleMember)

	invite := func(tok, email, role string) int {
		return doRequest(t, srv, http.MethodPost, orgURL("/invitations"), tok, map[string]string{"email": email, "role": role}).Code
	}
	if got := invite(memberTok, "a@example.com", "member"); got != http.StatusForbidden {
		t.Errorf("member inviting: %d", got)
	}
	if got := invite(adminTok, "a@example.com", "owner"); got != http.StatusForbidden {
		t.Errorf("admin inviting an owner: %d", got)
	}
	if got := invite(adminTok, "not-an-email", "member"); got != http.StatusUnprocessableEntity {
		t.Errorf("bad e-mail: %d", got)
	}
	if got := invite(adminTok, "MEMBER1@example.com", "member"); got != http.StatusConflict {
		t.Errorf("already a member (any case): %d", got)
	}
	before := len(mails.Sent())
	if got := invite(adminTok, "guest@example.com", "member"); got != http.StatusCreated {
		t.Fatalf("invite: %d", got)
	}
	if len(mails.Sent()) != before+1 || mails.Sent()[before].To != "guest@example.com" {
		t.Fatalf("invitation e-mail not sent: %+v", mails.Sent())
	}
}

func TestInviteMailFailureStoresNothing(t *testing.T) {
	srv, mails := newTestServerWithMail(t)
	adminTok := newMemberToken(t, srv, "admin1", paneldb.RoleAdmin)
	mails.Err = errors.New("smtp down")
	rec := doRequest(t, srv, http.MethodPost, orgURL("/invitations"), adminTok, map[string]string{"email": "guest@example.com", "role": "member"})
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("got %d, want 502", rec.Code)
	}
	var list []paneldb.Invitation
	_ = json.Unmarshal(doRequest(t, srv, http.MethodGet, orgURL("/invitations"), adminTok, nil).Body.Bytes(), &list)
	if len(list) != 0 {
		t.Fatalf("invitation kept after failed send: %+v", list)
	}
}

func TestInviteWithoutMailReturnsLink(t *testing.T) {
	srv := newTestServer(t)
	adminTok := newMemberToken(t, srv, "admin1", paneldb.RoleAdmin)
	rec := doRequest(t, srv, http.MethodPost, orgURL("/invitations"), adminTok, map[string]string{"email": "guest@example.com", "role": "member"})
	var body struct {
		InviteURL string `json:"inviteUrl"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusCreated || body.InviteURL == "" {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func TestAcceptInvitationCreatesAccount(t *testing.T) {
	srv, mails := newTestServerWithMail(t)
	adminTok := newMemberToken(t, srv, "admin1", paneldb.RoleAdmin)
	doRequest(t, srv, http.MethodPost, orgURL("/invitations"), adminTok, map[string]string{"email": "Guest@Example.com", "role": "member"})
	tok := lastLinkToken(t, mails)

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/invitations/lookup", "", map[string]string{"token": tok})
	var preview struct {
		OrgSlug       string `json:"orgSlug"`
		Email         string `json:"email"`
		AccountExists bool   `json:"accountExists"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &preview)
	if rec.Code != http.StatusOK || preview.OrgSlug != testOrgSlug || preview.Email != "guest@example.com" || preview.AccountExists {
		t.Fatalf("lookup: %d %s", rec.Code, rec.Body)
	}

	if got := doRequest(t, srv, http.MethodPost, "/api/v1/invitations/accept", "", map[string]string{"token": tok, "username": "bad@name", "password": "password1"}).Code; got != http.StatusUnprocessableEntity {
		t.Fatalf("username with @: %d", got)
	}
	if got := doRequest(t, srv, http.MethodPost, "/api/v1/invitations/accept", "", map[string]string{"token": tok, "username": "admin1", "password": "password1"}).Code; got != http.StatusConflict {
		t.Fatalf("taken username: %d", got)
	}
	rec = doRequest(t, srv, http.MethodPost, "/api/v1/invitations/accept", "", map[string]string{"token": tok, "username": "guest", "password": "password1", "displayName": "Guest P"})
	var login loginResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	if rec.Code != http.StatusCreated || login.Token == "" || login.User.Email != "guest@example.com" {
		t.Fatalf("accept: %d %s", rec.Code, rec.Body)
	}
	if got := doRequest(t, srv, http.MethodGet, orgURL("/members"), login.Token, nil).Code; got != http.StatusOK {
		t.Fatalf("new member cannot see the org: %d", got)
	}
	if got := doRequest(t, srv, http.MethodPost, "/api/v1/invitations/lookup", "", map[string]string{"token": tok}).Code; got != http.StatusNotFound {
		t.Fatalf("used invitation still looks valid: %d", got)
	}
}

func TestAcceptInvitationExistingAccountNeedsThatAccount(t *testing.T) {
	srv, mails := newTestServerWithMail(t)
	adminTok := newMemberToken(t, srv, "admin1", paneldb.RoleAdmin)
	guestTok := sessionFor(t, srv, "guest") // guest@example.com, not in the org
	otherTok := sessionFor(t, srv, "other")
	doRequest(t, srv, http.MethodPost, orgURL("/invitations"), adminTok, map[string]string{"email": "guest@example.com", "role": "admin"})
	tok := lastLinkToken(t, mails)

	if got := doRequest(t, srv, http.MethodPost, "/api/v1/invitations/accept", "", map[string]string{"token": tok}).Code; got != http.StatusUnauthorized {
		t.Fatalf("no session: %d", got)
	}
	if got := doRequest(t, srv, http.MethodPost, "/api/v1/invitations/accept", otherTok, map[string]string{"token": tok}).Code; got != http.StatusForbidden {
		t.Fatalf("other account: %d", got)
	}
	if got := doRequest(t, srv, http.MethodPost, "/api/v1/invitations/accept", guestTok, map[string]string{"token": tok}).Code; got != http.StatusOK {
		t.Fatalf("right account: %d", got)
	}
	u, _ := srv.DB.GetUserByUsername(context.Background(), "guest")
	org, _ := srv.DB.GetOrgBySlug(context.Background(), testOrgSlug)
	if role, _ := srv.DB.GetMembership(context.Background(), org.ID, u.ID); role != paneldb.RoleAdmin {
		t.Fatalf("role = %q", role)
	}
}

func TestRevokeAndResendInvitation(t *testing.T) {
	srv, mails := newTestServerWithMail(t)
	adminTok := newMemberToken(t, srv, "admin1", paneldb.RoleAdmin)
	doRequest(t, srv, http.MethodPost, orgURL("/invitations"), adminTok, map[string]string{"email": "guest@example.com", "role": "member"})
	first := lastLinkToken(t, mails)
	var list []paneldb.Invitation
	_ = json.Unmarshal(doRequest(t, srv, http.MethodGet, orgURL("/invitations"), adminTok, nil).Body.Bytes(), &list)
	id := list[0].ID

	if got := doRequest(t, srv, http.MethodPost, orgURL(fmt.Sprintf("/invitations/%d/resend", id)), adminTok, nil).Code; got != http.StatusOK {
		t.Fatalf("resend: %d", got)
	}
	if lastLinkToken(t, mails) == first {
		t.Fatal("resend reused the token")
	}
	if got := doRequest(t, srv, http.MethodDelete, orgURL(fmt.Sprintf("/invitations/%d", id)), adminTok, nil).Code; got != http.StatusNoContent {
		t.Fatalf("revoke: %d", got)
	}
	if got := doRequest(t, srv, http.MethodPost, "/api/v1/invitations/lookup", "", map[string]string{"token": lastLinkToken(t, mails)}).Code; got != http.StatusNotFound {
		t.Fatalf("revoked invitation still valid: %d", got)
	}
}

func TestMembersEmailOnlyForAdmins(t *testing.T) {
	srv := newTestServer(t)
	adminTok := newMemberToken(t, srv, "admin1", paneldb.RoleAdmin)
	memberTok := newMemberToken(t, srv, "member1", paneldb.RoleMember)
	var asAdmin, asMember []paneldb.Member
	_ = json.Unmarshal(doRequest(t, srv, http.MethodGet, orgURL("/members"), adminTok, nil).Body.Bytes(), &asAdmin)
	_ = json.Unmarshal(doRequest(t, srv, http.MethodGet, orgURL("/members"), memberTok, nil).Body.Bytes(), &asMember)
	if asAdmin[0].Email == "" {
		t.Error("admin does not see e-mails")
	}
	for _, m := range asMember {
		if m.Email != "" {
			t.Errorf("member sees %s's e-mail", m.Username)
		}
	}
}

func TestCreateOrgOwnerByEmail(t *testing.T) {
	srv, mails := newTestServerWithMail(t)
	admin := adminToken(t, srv)
	sessionFor(t, srv, "existing")
	quota := map[string]any{"cpu": "4", "memory": "8Gi", "storage": "50Gi", "maxGameServers": 3}

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/orgs", admin, map[string]any{"slug": "one", "name": "One", "ownerEmail": "EXISTING@example.com", "quota": quota})
	if rec.Code != http.StatusCreated {
		t.Fatalf("existing owner: %d %s", rec.Code, rec.Body)
	}
	org, _ := srv.DB.GetOrgBySlug(context.Background(), "one")
	u, _ := srv.DB.GetUserByUsername(context.Background(), "existing")
	if role, _ := srv.DB.GetMembership(context.Background(), org.ID, u.ID); role != paneldb.RoleOwner {
		t.Fatalf("existing user role = %q", role)
	}

	before := len(mails.Sent())
	rec = doRequest(t, srv, http.MethodPost, "/api/v1/orgs", admin, map[string]any{"slug": "two", "name": "Two", "ownerEmail": "newcomer@example.com", "quota": quota})
	if rec.Code != http.StatusCreated || len(mails.Sent()) != before+1 {
		t.Fatalf("new owner: %d %s, mails %d", rec.Code, rec.Body, len(mails.Sent())-before)
	}
	org2, _ := srv.DB.GetOrgBySlug(context.Background(), "two")
	members, _ := srv.DB.ListMembers(context.Background(), org2.ID)
	if len(members) != 0 {
		t.Fatalf("org should start empty, got %+v", members)
	}
}
