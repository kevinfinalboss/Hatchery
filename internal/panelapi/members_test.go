package panelapi

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

func userID(t *testing.T, srv *Server, name string) int64 {
	t.Helper()
	u, err := srv.DB.GetUserByUsername(t.Context(), name)
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func TestMemberManagementRules(t *testing.T) {
	srv := newTestServer(t)
	owner := newMemberToken(t, srv, "owner1", paneldb.RoleOwner)
	adminTok := newMemberToken(t, srv, "admin1", paneldb.RoleAdmin)
	memberTok := newMemberToken(t, srv, "member1", paneldb.RoleMember)
	if _, err := srv.DB.CreateUser(t.Context(), "newbie", "password", false); err != nil {
		t.Fatal(err)
	}

	add := func(tok, username, role string) int {
		return doRequest(t, srv, http.MethodPost, orgURL("/members"), tok, map[string]any{"username": username, "role": role}).Code
	}
	if got := add(memberTok, "newbie", "member"); got != http.StatusForbidden {
		t.Errorf("a plain member cannot add members: got %d", got)
	}
	if got := add(adminTok, "newbie", "owner"); got != http.StatusForbidden {
		t.Errorf("an org admin cannot create an owner: got %d", got)
	}
	if got := add(adminTok, "ghost", "member"); got != http.StatusNotFound {
		t.Errorf("unknown username: got %d, want 404", got)
	}
	if got := add(adminTok, "newbie", "superuser"); got != http.StatusBadRequest {
		t.Errorf("invalid role: got %d, want 400", got)
	}
	if got := add(adminTok, "newbie", "member"); got != http.StatusCreated {
		t.Fatalf("admin adding a member: got %d", got)
	}
	if got := add(adminTok, "newbie", "member"); got != http.StatusConflict {
		t.Errorf("adding twice: got %d, want 409", got)
	}
	if got := add(owner, "newbie", "owner"); got != http.StatusConflict {
		t.Errorf("owner adding an existing member: got %d, want 409", got)
	}

	newbie := userID(t, srv, "newbie")
	setRole := func(tok string, uid int64, role string) int {
		return doRequest(t, srv, http.MethodPatch, orgURL(fmt.Sprintf("/members/%d", uid)), tok, map[string]any{"role": role}).Code
	}
	if got := setRole(adminTok, newbie, "owner"); got != http.StatusForbidden {
		t.Errorf("an admin cannot promote to owner: got %d", got)
	}
	if got := setRole(adminTok, userID(t, srv, "owner1"), "member"); got != http.StatusForbidden {
		t.Errorf("an admin cannot demote an owner: got %d", got)
	}
	if got := setRole(owner, newbie, "admin"); got != http.StatusOK {
		t.Errorf("owner promoting to admin: got %d", got)
	}
}

func TestRemoveMemberRulesAndLastOwner(t *testing.T) {
	srv := newTestServer(t)
	owner := newMemberToken(t, srv, "owner1", paneldb.RoleOwner)
	adminTok := newMemberToken(t, srv, "admin1", paneldb.RoleAdmin)
	memberTok := newMemberToken(t, srv, "member1", paneldb.RoleMember)
	member2 := newMemberToken(t, srv, "member2", paneldb.RoleMember)
	_ = member2

	del := func(tok string, uid int64) int {
		return doRequest(t, srv, http.MethodDelete, orgURL(fmt.Sprintf("/members/%d", uid)), tok, nil).Code
	}
	if got := del(memberTok, userID(t, srv, "member2")); got != http.StatusForbidden {
		t.Errorf("a member cannot remove someone else: got %d", got)
	}
	if got := del(adminTok, userID(t, srv, "owner1")); got != http.StatusForbidden {
		t.Errorf("an admin cannot remove an owner: got %d", got)
	}
	if got := del(memberTok, userID(t, srv, "member1")); got != http.StatusNoContent {
		t.Errorf("a member can leave: got %d", got)
	}
	if got := del(adminTok, userID(t, srv, "member2")); got != http.StatusNoContent {
		t.Errorf("an admin removing a member: got %d", got)
	}

	// The fixture owner and owner1 are the two owners: owner1 may leave, then the fixture owner is last.
	if got := del(owner, userID(t, srv, "owner1")); got != http.StatusNoContent {
		t.Fatalf("an owner leaving while another owner exists: got %d", got)
	}
	if got := doRequest(t, srv, http.MethodDelete, orgURL(fmt.Sprintf("/members/%d", userID(t, srv, "fixture-owner"))),
		adminToken(t, srv), nil).Code; got != http.StatusConflict {
		t.Errorf("removing the last owner: got %d, want 409", got)
	}
}

func TestListMembers(t *testing.T) {
	srv := newTestServer(t)
	tok := newMemberToken(t, srv, "m", paneldb.RoleMember)
	rec := doRequest(t, srv, http.MethodGet, orgURL("/members"), tok, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	if !contains(rec.Body.String(), `"fixture-owner"`) || !contains(rec.Body.String(), `"role":"owner"`) {
		t.Fatalf("expected the fixture owner in the list, got %s", rec.Body.String())
	}
}
