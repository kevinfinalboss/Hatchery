package panelapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

// jsonField unmarshals body into a map and returns the given key as a string.
func jsonField(t *testing.T, body []byte, key string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	v, _ := m[key].(string)
	return v
}

// gsFixtureWithTarget is gsFixture with spec.backupTarget.connection set to conn.
func gsFixtureWithTarget(name, conn string) *gameserversv1alpha1.GameServer {
	gs := gsFixture(name)
	gs.Spec.BackupTarget = &gameserversv1alpha1.BackupTarget{Connection: conn}
	return gs
}

// testTenantWithBackups is testTenant() with a platform backup quota of maxPerServer per server.
func testTenantWithBackups(maxPerServer int32) *gameserversv1alpha1.Tenant {
	tenant := testTenant()
	tenant.Spec.Quota.Backups = &gameserversv1alpha1.TenantBackupQuota{MaxPerServer: maxPerServer, MaxPerOrg: 100, RetentionDays: 7}
	return tenant
}

func dailyRestart() map[string]any {
	return map[string]any{"displayName": "Reinício diário", "cron": "0 4 * * *", "timeZone": "America/Sao_Paulo",
		"tasks": []map[string]any{{"action": "Command", "command": "say bye"}, {"action": "Restart", "delaySeconds": 60}}}
}

func TestScheduleCRUDAndRunNow(t *testing.T) {
	srv := newTestServer(t, gsFixture("mc"))
	adm := newMemberToken(t, srv, "adm", paneldb.RoleAdmin)

	rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers/mc/schedules"), adm, dailyRestart())
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	name := jsonField(t, rec.Body.Bytes(), "name")
	if !strings.HasPrefix(name, "mc-") {
		t.Fatalf("generated name %q", name)
	}
	if rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers/mc/schedules/"+name+"/run"), adm, nil); rec.Code != http.StatusAccepted {
		t.Fatalf("run: %d", rec.Code)
	}
	upd := dailyRestart()
	upd["suspend"] = true
	if rec := doRequest(t, srv, http.MethodPut, orgURL("/gameservers/mc/schedules/"+name), adm, upd); rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doRequest(t, srv, http.MethodDelete, orgURL("/gameservers/mc/schedules/"+name), adm, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}
}

func TestScheduleValidationAndLimits(t *testing.T) {
	srv := newTestServer(t, gsFixture("mc"))
	adm := newMemberToken(t, srv, "adm", paneldb.RoleAdmin)
	bad := dailyRestart()
	bad["cron"] = "* * * * *"
	if rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers/mc/schedules"), adm, bad); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("every-minute cron: %d, want 422", rec.Code)
	}
	for i := 0; i < 10; i++ {
		if rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers/mc/schedules"), adm, dailyRestart()); rec.Code != http.StatusCreated {
			t.Fatalf("create #%d: %d %s", i, rec.Code, rec.Body.String())
		}
	}
	if rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers/mc/schedules"), adm, dailyRestart()); rec.Code != http.StatusConflict {
		t.Fatalf("11th schedule: %d, want 409", rec.Code)
	}
}

func TestSchedulesNeedThePermission(t *testing.T) {
	srv := newTestServer(t, gsFixture("mc"))
	power := memberWithGrants(t, srv, "p", paneldb.Grant{GameServer: "mc", Permissions: []paneldb.Permission{paneldb.PermPower}})
	if rec := doRequest(t, srv, http.MethodGet, orgURL("/gameservers/mc/schedules"), power, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("without schedules permission: %d, want 403", rec.Code)
	}
	sched := memberWithGrants(t, srv, "s", paneldb.Grant{GameServer: "mc", Permissions: []paneldb.Permission{paneldb.PermSchedules}})
	if rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers/mc/schedules"), sched, dailyRestart()); rec.Code != http.StatusCreated {
		t.Fatalf("with schedules permission: %d %s", rec.Code, rec.Body.String())
	}
}

func TestPlatformBackupScheduleNeedsHeadroom(t *testing.T) {
	// Server targets the platform connection; the tenant allows 3 backups per server.
	srv := newTestServer(t, gsFixtureWithTarget("mc", "platform"), testTenantWithBackups(3))
	srv.Backup = BackupConfig{Bucket: "plat", SecretNamespace: "hatchery-system", SecretName: "plat"}
	adm := newMemberToken(t, srv, "adm", paneldb.RoleAdmin)
	body := map[string]any{"cron": "0 4 * * *", "tasks": []map[string]any{{"action": "Backup", "keepLast": 3}}}
	if rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers/mc/schedules"), adm, body); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("keepLast == maxPerServer: %d, want 422", rec.Code)
	}
	body["tasks"] = []map[string]any{{"action": "Backup", "keepLast": 2}}
	if rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers/mc/schedules"), adm, body); rec.Code != http.StatusCreated {
		t.Fatalf("keepLast < maxPerServer: %d %s", rec.Code, rec.Body.String())
	}
}
