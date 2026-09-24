package panelapi

import (
	"encoding/json"
	"net/http"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

func crashyServer(name string) *gameserversv1alpha1.GameServer {
	return &gameserversv1alpha1.GameServer{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testOrgNS()},
		Spec: gameserversv1alpha1.GameServerSpec{
			EggRef:  gameserversv1alpha1.GameServerEggRef{Name: "vegg"},
			State:   gameserversv1alpha1.GameServerStateRunning,
			Storage: gameserversv1alpha1.GameServerStorage{Size: "1Gi"},
		},
	}
}

func TestCrashLogServesTheSavedConsole(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: gameserversv1alpha1.CrashLogConfigMapName("crashy"), Namespace: testOrgNS()},
		Data: map[string]string{
			gameserversv1alpha1.CrashLogKeyLog:       "Exception in server tick loop\n",
			gameserversv1alpha1.CrashLogKeyAt:        "2026-09-24T14:02:00Z",
			gameserversv1alpha1.CrashLogKeyExitCode:  "137",
			gameserversv1alpha1.CrashLogKeyReason:    "OOMKilled",
			gameserversv1alpha1.CrashLogKeyOOMKilled: "true",
		},
	}
	srv := newTestServer(t, customizeEgg(), crashyServer("crashy"), crashyServer("fine"), cm)
	reader := memberWithGrants(t, srv, "reader", paneldb.Grant{GameServer: "crashy", Permissions: []paneldb.Permission{paneldb.PermConsoleRead}})
	files := memberWithGrants(t, srv, "files", paneldb.Grant{GameServer: "crashy", Permissions: []paneldb.Permission{paneldb.PermFilesRead}})
	admin := newMemberToken(t, srv, "adm", paneldb.RoleAdmin)

	rec := doRequest(t, srv, http.MethodGet, orgURL("/gameservers/crashy/crash-log"), reader, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("console.read must read the crash log, got %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		At        string `json:"at"`
		ExitCode  int32  `json:"exitCode"`
		Reason    string `json:"reason"`
		OOMKilled bool   `json:"oomKilled"`
		Log       string `json:"log"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.ExitCode != 137 || !out.OOMKilled || out.Reason != "OOMKilled" || out.At != "2026-09-24T14:02:00Z" || out.Log != "Exception in server tick loop\n" {
		t.Fatalf("got %+v", out)
	}

	if rec := doRequest(t, srv, http.MethodGet, orgURL("/gameservers/crashy/crash-log"), files, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("without console.read: want 403, got %d", rec.Code)
	}
	if rec := doRequest(t, srv, http.MethodGet, orgURL("/gameservers/fine/crash-log"), admin, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("a server that never crashed: want 404, got %d", rec.Code)
	}
}
