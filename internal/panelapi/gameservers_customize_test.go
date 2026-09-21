package panelapi

import (
	"encoding/json"
	"net/http"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

func customizeEgg() *gameserversv1alpha1.Egg {
	return &gameserversv1alpha1.Egg{
		ObjectMeta: metav1.ObjectMeta{Name: "vegg", Namespace: testOrgNS()},
		Spec: gameserversv1alpha1.EggSpec{
			Images: []gameserversv1alpha1.EggImage{{Name: "default", Image: "example.com/g:1"}, {Name: "alt", Image: "example.com/g:2"}}, StartCommand: "run",
			Variables: []gameserversv1alpha1.EggVariable{
				{Name: "MOTD", UserEditable: true, Default: "hi"},
				{Name: "PORT", UserEditable: true, ValidationRegex: `^[0-9]+$`, Default: "7777"},
				{Name: "LOCKED", Default: "x"},
			},
		},
	}
}

func displayBody(displayName string, extra map[string]any) map[string]any {
	spec := map[string]any{
		"displayName": displayName,
		"eggRef":      map[string]any{"name": "vegg"},
		"storage":     map[string]any{"size": "1Gi"},
	}
	for k, v := range extra {
		spec[k] = v
	}
	return map[string]any{"spec": spec}
}

func TestCreateGameServerFromDisplayNameGeneratesAUniqueSlug(t *testing.T) {
	srv := newTestServer(t, customizeEgg())
	admin := newMemberToken(t, srv, "adm", paneldb.RoleAdmin)

	names := []string{}
	for range 2 {
		rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers"), admin, displayBody("Survival dos Amigos", nil))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create: got %d: %s", rec.Code, rec.Body.String())
		}
		var gs gameserversv1alpha1.GameServer
		if err := json.Unmarshal(rec.Body.Bytes(), &gs); err != nil {
			t.Fatal(err)
		}
		names = append(names, gs.Name)
	}
	if names[0] != "survival-dos-amigos" || names[1] != "survival-dos-amigos-2" {
		t.Fatalf("got slugs %v", names)
	}
}

func TestCreateGameServerNeedsANameOrADisplayName(t *testing.T) {
	srv := newTestServer(t, customizeEgg())
	admin := newMemberToken(t, srv, "adm", paneldb.RoleAdmin)
	rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers"), admin, displayBody("", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateGameServerRejectsAControlCharacterInTheDisplayName(t *testing.T) {
	srv := newTestServer(t, customizeEgg())
	admin := newMemberToken(t, srv, "adm", paneldb.RoleAdmin)
	rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers"), admin, displayBody("a\x07b", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateGameServerValidatesVariablesAgainstTheEgg(t *testing.T) {
	srv := newTestServer(t, customizeEgg())
	admin := newMemberToken(t, srv, "adm", paneldb.RoleAdmin)

	bad := displayBody("Bad", map[string]any{"variables": []map[string]string{{"name": "LOCKED", "value": "y"}}})
	if rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers"), admin, bad); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("non-editable variable: got %d, want 422: %s", rec.Code, rec.Body.String())
	}
	good := displayBody("Good", map[string]any{"variables": []map[string]string{{"name": "PORT", "value": "25565"}}})
	if rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers"), admin, good); rec.Code != http.StatusCreated {
		t.Fatalf("valid variable: got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateGameServerReservesWhatItLimits(t *testing.T) {
	srv := newTestServer(t, customizeEgg())
	admin := newMemberToken(t, srv, "adm", paneldb.RoleAdmin)
	body := displayBody("Sized", map[string]any{"resources": map[string]any{"limits": map[string]string{"cpu": "2", "memory": "4Gi"}}})
	if rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers"), admin, body); rec.Code != http.StatusCreated {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	var got gameserversv1alpha1.GameServer
	if err := srv.Client.Get(t.Context(), client.ObjectKey{Namespace: testOrgNS(), Name: "sized"}, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Spec.Resources.Requests.Cpu().Equal(resource.MustParse("2")) || !got.Spec.Resources.Requests.Memory().Equal(resource.MustParse("4Gi")) {
		t.Fatalf("requests must equal limits, got %+v", got.Spec.Resources)
	}
}

func editableServer() *gameserversv1alpha1.GameServer {
	return &gameserversv1alpha1.GameServer{
		ObjectMeta: metav1.ObjectMeta{Name: "edit-me", Namespace: testOrgNS()},
		Spec: gameserversv1alpha1.GameServerSpec{
			EggRef:  gameserversv1alpha1.GameServerEggRef{Name: "vegg"},
			Storage: gameserversv1alpha1.GameServerStorage{Size: "1Gi"},
		},
	}
}

func TestUpdateGameServer(t *testing.T) {
	srv := newTestServer(t, customizeEgg(), editableServer())
	admin := newMemberToken(t, srv, "adm", paneldb.RoleAdmin)
	member := newMemberToken(t, srv, "mem", paneldb.RoleMember)
	url := orgURL("/gameservers/edit-me")

	if rec := doRequest(t, srv, http.MethodPatch, url, member, map[string]any{"displayName": "Nope"}); rec.Code != http.StatusForbidden {
		t.Fatalf("member: got %d, want 403: %s", rec.Code, rec.Body.String())
	}

	rec := doRequest(t, srv, http.MethodPatch, url, admin, map[string]any{
		"displayName": "Renamed",
		"variables":   []map[string]string{{"name": "MOTD", "value": "hello"}},
		"resources":   map[string]any{"limits": map[string]string{"cpu": "1", "memory": "1Gi"}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("admin: got %d: %s", rec.Code, rec.Body.String())
	}
	var got gameserversv1alpha1.GameServer
	if err := srv.Client.Get(t.Context(), client.ObjectKey{Namespace: testOrgNS(), Name: "edit-me"}, &got); err != nil {
		t.Fatal(err)
	}
	if got.Spec.DisplayName != "Renamed" || len(got.Spec.Variables) != 1 || got.Spec.Variables[0].Value != "hello" {
		t.Fatalf("update not applied: %+v", got.Spec)
	}
	if !got.Spec.Resources.Requests.Cpu().Equal(resource.MustParse("1")) {
		t.Fatalf("requests must follow limits, got %+v", got.Spec.Resources)
	}

	locked := map[string]any{"variables": []map[string]string{{"name": "LOCKED", "value": "y"}}}
	if rec := doRequest(t, srv, http.MethodPatch, url, admin, locked); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("non-editable variable: got %d, want 422: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateGameServerCannotExceedTheQuota(t *testing.T) {
	busy := existingServer("busy", "1Gi")
	busy.Spec.Resources = corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("3")}}
	srv := newTestServer(t, customizeEgg(), tenantWithQuota(10, "100Gi"), editableServer(), busy)
	admin := newMemberToken(t, srv, "adm", paneldb.RoleAdmin)

	over := map[string]any{"resources": map[string]any{"limits": map[string]string{"cpu": "2"}}} // 3 + 2 > 4
	if rec := doRequest(t, srv, http.MethodPatch, orgURL("/gameservers/edit-me"), admin, over); rec.Code != http.StatusConflict {
		t.Fatalf("got %d, want 409: %s", rec.Code, rec.Body.String())
	}
	fits := map[string]any{"resources": map[string]any{"limits": map[string]string{"cpu": "1"}}} // 3 + 1 = 4
	if rec := doRequest(t, srv, http.MethodPatch, orgURL("/gameservers/edit-me"), admin, fits); rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

func TestGetQuotaReportsLimitAndUsage(t *testing.T) {
	used := existingServer("used", "5Gi")
	used.Spec.Resources = corev1.ResourceRequirements{Requests: corev1.ResourceList{
		corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("2Gi"),
	}}
	srv := newTestServer(t, tenantWithQuota(3, "20Gi"), used)
	member := newMemberToken(t, srv, "mem", paneldb.RoleMember)

	rec := doRequest(t, srv, http.MethodGet, orgURL("/quota"), member, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	var got quotaUsageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Limit.MaxGameServers != 3 || got.Limit.Storage != "20Gi" {
		t.Fatalf("limit: %+v", got.Limit)
	}
	if got.Used.CPU != "1" || got.Used.Memory != "2Gi" || got.Used.Storage != "5Gi" || got.Used.GameServers != 1 {
		t.Fatalf("used: %+v", got.Used)
	}
}

func TestGameServerImageNameIsCheckedAgainstTheEgg(t *testing.T) {
	srv := newTestServer(t, customizeEgg(), editableServer())
	admin := newMemberToken(t, srv, "adm", paneldb.RoleAdmin)

	bad := displayBody("Bad image", map[string]any{"imageName": "nope"})
	if rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers"), admin, bad); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("create with an undeclared image: got %d, want 422: %s", rec.Code, rec.Body.String())
	}
	good := displayBody("Good image", map[string]any{"imageName": "alt"})
	if rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers"), admin, good); rec.Code != http.StatusCreated {
		t.Fatalf("create with a declared image: got %d: %s", rec.Code, rec.Body.String())
	}

	url := orgURL("/gameservers/edit-me")
	if rec := doRequest(t, srv, http.MethodPatch, url, admin, map[string]any{"imageName": "nope"}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("patch to an undeclared image: got %d, want 422: %s", rec.Code, rec.Body.String())
	}
	if rec := doRequest(t, srv, http.MethodPatch, url, admin, map[string]any{"imageName": "alt"}); rec.Code != http.StatusOK {
		t.Fatalf("patch to a declared image: got %d: %s", rec.Code, rec.Body.String())
	}
	var got gameserversv1alpha1.GameServer
	if err := srv.Client.Get(t.Context(), client.ObjectKey{Namespace: testOrgNS(), Name: "edit-me"}, &got); err != nil {
		t.Fatal(err)
	}
	if got.Spec.ImageName != "alt" {
		t.Fatalf("imageName not stored: %+v", got.Spec)
	}
}

func TestReinstallGameServer(t *testing.T) {
	running := editableServer()
	running.Spec.State = gameserversv1alpha1.GameServerStateRunning
	stopped := editableServer()
	stopped.Name = "stopped-one"
	stopped.Spec.State = gameserversv1alpha1.GameServerStateStopped
	srv := newTestServer(t, customizeEgg(), running, stopped)
	admin := newMemberToken(t, srv, "adm", paneldb.RoleAdmin)
	member := newMemberToken(t, srv, "mem", paneldb.RoleMember)

	if rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers/edit-me/reinstall"), member, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("member: got %d, want 403", rec.Code)
	}
	if rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers/missing/reinstall"), admin, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown server: got %d, want 404", rec.Code)
	}

	get := func(name string) gameserversv1alpha1.GameServer {
		var gs gameserversv1alpha1.GameServer
		if err := srv.Client.Get(t.Context(), client.ObjectKey{Namespace: testOrgNS(), Name: name}, &gs); err != nil {
			t.Fatal(err)
		}
		return gs
	}

	if rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers/edit-me/reinstall"), admin, nil); rec.Code != http.StatusAccepted {
		t.Fatalf("running server: got %d, want 202: %s", rec.Code, rec.Body.String())
	}
	gs := get("edit-me")
	if gs.Spec.InstallRevision != 1 {
		t.Fatalf("installRevision = %d, want 1", gs.Spec.InstallRevision)
	}
	if gs.Annotations[gameserversv1alpha1.RestartAnnotation] == "" {
		t.Fatal("a running server must be restarted so the new revision takes effect")
	}

	if rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers/stopped-one/reinstall"), admin, nil); rec.Code != http.StatusAccepted {
		t.Fatalf("stopped server: got %d, want 202: %s", rec.Code, rec.Body.String())
	}
	gs = get("stopped-one")
	if gs.Spec.InstallRevision != 1 || gs.Annotations[gameserversv1alpha1.RestartAnnotation] != "" {
		t.Fatalf("a stopped server only gets the new revision, for its next start: %+v", gs.ObjectMeta.Annotations)
	}
}

func TestLogsRejectsAnUnknownContainer(t *testing.T) {
	srv := newTestServer(t, customizeEgg(), editableServer())
	admin := newMemberToken(t, srv, "adm", paneldb.RoleAdmin)
	if rec := doRequest(t, srv, http.MethodGet, orgURL("/gameservers/edit-me/logs?container=sftp-agent"), admin, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestEggWithAnInvalidStartupRegexIsRejected(t *testing.T) {
	srv := newTestServer(t)
	admin := newMemberToken(t, srv, "adm", paneldb.RoleAdmin)
	body := map[string]any{"name": "bad", "spec": map[string]any{
		"images":           []map[string]string{{"name": "default", "image": "x:1"}},
		"startCommand":     "run",
		"startupDetection": map[string]any{"regex": "(unclosed"},
	}}
	if rec := doRequest(t, srv, http.MethodPost, orgURL("/eggs"), admin, body); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("got %d, want 422: %s", rec.Code, rec.Body.String())
	}
}
