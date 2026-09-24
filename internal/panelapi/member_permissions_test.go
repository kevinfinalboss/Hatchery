package panelapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

func TestPutMemberPermissions(t *testing.T) {
	srv := newTestServer(t, gsFixture("mc"))
	admin := newMemberToken(t, srv, "adm", paneldb.RoleAdmin)
	newMemberToken(t, srv, "m", paneldb.RoleMember)
	u, _ := srv.DB.GetUserByUsername(context.Background(), "m")
	path := orgURL("/members/" + strconv.FormatInt(u.ID, 10) + "/permissions")

	ok := map[string]any{"grants": []map[string]any{
		{"gameserver": "mc", "permissions": []string{"power", "power", "console.read"}},
		{"gameserver": "*", "permissions": []string{"files.read"}},
	}}
	if rec := doRequest(t, srv, http.MethodPut, path, admin, ok); rec.Code != http.StatusOK {
		t.Fatalf("put: %d %s", rec.Code, rec.Body.String())
	}
	rec := doRequest(t, srv, http.MethodGet, path, admin, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"gameserver":"*"`) {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}

	for name, body := range map[string]map[string]any{
		"unknown permission": {"grants": []map[string]any{{"gameserver": "mc", "permissions": []string{"files.delete"}}}},
		"unknown server":     {"grants": []map[string]any{{"gameserver": "ghost", "permissions": []string{"power"}}}},
	} {
		if rec := doRequest(t, srv, http.MethodPut, path, admin, body); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: got %d, want 422", name, rec.Code)
		}
	}

	member := newMemberToken(t, srv, "m2", paneldb.RoleMember)
	if rec := doRequest(t, srv, http.MethodPut, path, member, ok); rec.Code != http.StatusForbidden {
		t.Errorf("member editing grants: got %d, want 403", rec.Code)
	}
}

func TestDemotedAdminHasNoServerAccess(t *testing.T) {
	srv := newTestServer(t, gsFixture("mc"))
	owner := newMemberToken(t, srv, "own", paneldb.RoleOwner)
	newMemberToken(t, srv, "adm", paneldb.RoleAdmin)
	u, _ := srv.DB.GetUserByUsername(context.Background(), "adm")
	rec := doRequest(t, srv, http.MethodPatch, orgURL("/members/"+strconv.FormatInt(u.ID, 10)), owner, map[string]string{"role": "member"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"hasServerAccess":false`) {
		t.Fatalf("demote: %d %s", rec.Code, rec.Body.String())
	}
}
