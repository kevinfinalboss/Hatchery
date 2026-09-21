package panelapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

// backupFixture is a server with one org (testorg) that may use the platform storage.
type backupFixture struct {
	srv    *Server
	admin  string
	member string
}

func platformSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "platform-s3", Namespace: "hatchery-system"},
		Data:       map[string][]byte{"access-key": []byte("AKIA"), "secret-key": []byte("s3cr3t")},
	}
}

func newBackupFixture(t *testing.T, withQuota bool, objs ...client.Object) backupFixture {
	t.Helper()
	tenant := tenantWithQuota(10, "100Gi")
	if withQuota {
		tenant.Spec.Quota.Backups = &gameserversv1alpha1.TenantBackupQuota{MaxPerServer: 2, MaxPerOrg: 3, RetentionDays: 7}
	}
	all := append([]client.Object{customizeEgg(), editableServer(), tenant, platformSecret()}, objs...)
	srv := newTestServer(t, all...)
	srv.Backup = BackupConfig{Endpoint: "http://minio:9000", Bucket: "hatchery", SecretNamespace: "hatchery-system", SecretName: "platform-s3"}
	return backupFixture{
		srv:    srv,
		admin:  newMemberToken(t, srv, "adm", paneldb.RoleAdmin),
		member: newMemberToken(t, srv, "mem", paneldb.RoleMember),
	}
}

func (f backupFixture) backups(t *testing.T) []gameserversv1alpha1.GameServerBackup {
	t.Helper()
	var list gameserversv1alpha1.GameServerBackupList
	if err := f.srv.Client.List(t.Context(), &list, client.InNamespace(testOrgNS())); err != nil {
		t.Fatal(err)
	}
	return list.Items
}

func (f backupFixture) putConnection(t *testing.T, name string, body map[string]any) int {
	t.Helper()
	return doRequest(t, f.srv, http.MethodPut, orgURL("/backup-connections/"+name), f.admin, body).Code
}

func (f backupFixture) setTarget(t *testing.T, server string, target map[string]string) *httptestResult {
	t.Helper()
	rec := doRequest(t, f.srv, http.MethodPatch, orgURL("/gameservers/"+server), f.admin, map[string]any{"backupTarget": target})
	return &httptestResult{Code: rec.Code, Body: rec.Body.String()}
}

type httptestResult struct {
	Code int
	Body string
}

func (f backupFixture) create(t *testing.T, server string) int {
	t.Helper()
	return doRequest(t, f.srv, http.MethodPost, orgURL("/gameservers/"+server+"/backups"), f.admin, nil).Code
}

func s3Connection(buckets ...string) map[string]any {
	return map[string]any{"endpoint": "https://s3.example.com", "buckets": buckets, "accessKey": "MYKEY", "secretKey": "MYSECRET"}
}

func TestConnectionsAreOrgLevelAndNeverLeakKeys(t *testing.T) {
	f := newBackupFixture(t, true)
	if code := doRequest(t, f.srv, http.MethodPut, orgURL("/backup-connections/aws"), f.member, s3Connection("b1")).Code; code != http.StatusForbidden {
		t.Fatalf("member: got %d, want 403", code)
	}
	for _, bad := range []string{"Upper", "platform", "-x", "a_b"} {
		if code := f.putConnection(t, bad, s3Connection("b1")); code != http.StatusBadRequest {
			t.Fatalf("connection name %q: got %d, want 400", bad, code)
		}
	}
	if code := f.putConnection(t, "aws", s3Connection("NOT A BUCKET")); code != http.StatusBadRequest {
		t.Fatalf("invalid bucket name: got %d, want 400", code)
	}
	if code := f.putConnection(t, "aws", map[string]any{"buckets": []string{"b1"}}); code != http.StatusBadRequest {
		t.Fatalf("keys are required on create: got %d, want 400", code)
	}
	if code := f.putConnection(t, "aws", s3Connection()); code != http.StatusBadRequest {
		t.Fatalf("at least one bucket: got %d, want 400", code)
	}
	if code := f.putConnection(t, "aws", s3Connection("bucket-one", "bucket-two")); code != http.StatusOK {
		t.Fatalf("create: got %d", code)
	}
	if code := f.putConnection(t, "second", s3Connection("other-bucket")); code != http.StatusOK {
		t.Fatalf("a second connection: got %d", code)
	}

	rec := doRequest(t, f.srv, http.MethodGet, orgURL("/backup-settings"), f.member, nil)
	if strings.Contains(rec.Body.String(), "MYKEY") || strings.Contains(rec.Body.String(), "MYSECRET") {
		t.Fatalf("the keys must never be returned: %s", rec.Body.String())
	}
	var got backupSettingsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Platform.Available || len(got.Connections) != 2 || got.Connections[0].Name != "aws" ||
		len(got.Connections[0].Buckets) != 2 || got.Connections[0].Endpoint != "https://s3.example.com" {
		t.Fatalf("settings: %+v", got)
	}

	var sec corev1.Secret
	if err := f.srv.Client.Get(t.Context(), client.ObjectKey{Namespace: testOrgNS(), Name: "hatchery-s3-aws"}, &sec); err != nil {
		t.Fatal(err)
	}
	if len(sec.Data["restic-password"]) == 0 || string(sec.Data["access-key"]) != "MYKEY" {
		t.Fatalf("secret content: %v", sec.Data)
	}
	password := string(sec.Data["restic-password"])

	// Saving again without keys keeps them (and the restic password); the bucket list is replaced.
	if code := f.putConnection(t, "aws", map[string]any{"endpoint": "https://s3.example.com", "buckets": []string{"bucket-three"}}); code != http.StatusOK {
		t.Fatalf("update without keys: got %d", code)
	}
	_ = f.srv.Client.Get(t.Context(), client.ObjectKey{Namespace: testOrgNS(), Name: "hatchery-s3-aws"}, &sec)
	if string(sec.Data["access-key"]) != "MYKEY" || string(sec.Data["restic-password"]) != password || !strings.Contains(string(sec.Data["buckets"]), "bucket-three") {
		t.Fatalf("keys and password must be kept and the buckets replaced: %v", sec.Data)
	}
}

func TestServerNeedsADestinationBeforeItsFirstBackup(t *testing.T) {
	f := newBackupFixture(t, false)
	if code := f.create(t, "edit-me"); code != http.StatusConflict {
		t.Fatalf("no destination: got %d, want 409", code)
	}
	rec := doRequest(t, f.srv, http.MethodGet, orgURL("/backup-settings"), f.member, nil)
	var got backupSettingsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Platform.Available {
		t.Fatal("no backups block on the org means the platform storage is off for it")
	}
	if r := f.setTarget(t, "edit-me", map[string]string{"connection": "platform"}); r.Code != http.StatusConflict {
		t.Fatalf("choosing the platform without limits: got %d, want 409: %s", r.Code, r.Body)
	}
}

func TestBackupTargetIsValidated(t *testing.T) {
	f := newBackupFixture(t, true)
	f.putConnection(t, "aws", s3Connection("bucket-one"))

	cases := []struct {
		name   string
		target map[string]string
		want   int
	}{
		{"unknown connection", map[string]string{"connection": "nope", "bucket": "bucket-one"}, http.StatusUnprocessableEntity},
		{"bucket not in the list", map[string]string{"connection": "aws", "bucket": "bucket-two"}, http.StatusUnprocessableEntity},
		{"bucket required", map[string]string{"connection": "aws"}, http.StatusUnprocessableEntity},
		{"bad prefix", map[string]string{"connection": "aws", "bucket": "bucket-one", "prefix": "a b;c"}, http.StatusUnprocessableEntity},
		{"ok", map[string]string{"connection": "aws", "bucket": "bucket-one", "prefix": "/worlds/survival/"}, http.StatusOK},
		{"platform", map[string]string{"connection": "platform"}, http.StatusOK},
		{"cleared", map[string]string{"connection": ""}, http.StatusOK},
	}
	for _, c := range cases {
		if r := f.setTarget(t, "edit-me", c.target); r.Code != c.want {
			t.Fatalf("%s: got %d, want %d: %s", c.name, r.Code, c.want, r.Body)
		}
	}
	if gs := getServer(t, f.srv, "edit-me"); gs.Spec.BackupTarget != nil {
		t.Fatalf("an empty connection clears the target: %+v", gs.Spec.BackupTarget)
	}
	f.setTarget(t, "edit-me", map[string]string{"connection": "aws", "bucket": "bucket-one", "prefix": "/worlds/survival/"})
	if tg := getServer(t, f.srv, "edit-me").Spec.BackupTarget; tg == nil || tg.Prefix != "worlds/survival" {
		t.Fatalf("the prefix is stored without the surrounding slashes: %+v", tg)
	}
}

func TestCreatePlatformBackup(t *testing.T) {
	f := newBackupFixture(t, true)
	if r := f.setTarget(t, "edit-me", map[string]string{"connection": "platform"}); r.Code != http.StatusOK {
		t.Fatalf("target: got %d: %s", r.Code, r.Body)
	}
	if code := doRequest(t, f.srv, http.MethodPost, orgURL("/gameservers/edit-me/backups"), f.member, nil).Code; code != http.StatusForbidden {
		t.Fatalf("member: got %d, want 403", code)
	}
	if code := f.create(t, "edit-me"); code != http.StatusAccepted {
		t.Fatalf("got %d, want 202", code)
	}
	bs := f.backups(t)
	if len(bs) != 1 {
		t.Fatalf("expected one backup, got %d", len(bs))
	}
	b := bs[0]
	s3 := b.Spec.Destination.S3
	if b.Spec.GameServerRef.Name != "edit-me" || s3.Bucket != "hatchery" || s3.Endpoint != "http://minio:9000" ||
		s3.Prefix != testOrgSlug+"/edit-me" || s3.SecretRef.Name != platformBackupSecret {
		t.Fatalf("destination: %+v", b.Spec)
	}
	if b.Labels[gameserversv1alpha1.BackupGameServerLabel] != "edit-me" || b.Labels[gameserversv1alpha1.BackupDestinationLabel] != "platform" {
		t.Fatalf("labels: %v", b.Labels)
	}
	expires, err := time.Parse(time.RFC3339, b.Annotations[gameserversv1alpha1.BackupExpiresAtAnnotation])
	if err != nil || time.Until(expires) < 6*24*time.Hour || time.Until(expires) > 8*24*time.Hour {
		t.Fatalf("expiry must be about 7 days out, got %q (%v)", b.Annotations[gameserversv1alpha1.BackupExpiresAtAnnotation], err)
	}

	var sec corev1.Secret
	if err := f.srv.Client.Get(t.Context(), client.ObjectKey{Namespace: testOrgNS(), Name: platformBackupSecret}, &sec); err != nil {
		t.Fatalf("the org's platform secret must be created on demand: %v", err)
	}
	if string(sec.Data["access-key"]) != "AKIA" || string(sec.Data["secret-key"]) != "s3cr3t" || len(sec.Data["restic-password"]) < 16 {
		t.Fatalf("secret: %v", sec.Data)
	}
	first := string(sec.Data["restic-password"])
	if code := f.create(t, "edit-me"); code != http.StatusAccepted {
		t.Fatalf("second backup: got %d", code)
	}
	_ = f.srv.Client.Get(t.Context(), client.ObjectKey{Namespace: testOrgNS(), Name: platformBackupSecret}, &sec)
	if string(sec.Data["restic-password"]) != first {
		t.Fatal("the restic password must be generated once and kept: changing it would orphan every snapshot")
	}
}

func TestPlatformBackupLimits(t *testing.T) {
	f := newBackupFixture(t, true, existingServer("other", "1Gi"))
	for _, s := range []string{"edit-me", "other"} {
		if r := f.setTarget(t, s, map[string]string{"connection": "platform"}); r.Code != http.StatusOK {
			t.Fatalf("target %s: got %d: %s", s, r.Code, r.Body)
		}
	}
	if f.create(t, "edit-me") != http.StatusAccepted || f.create(t, "edit-me") != http.StatusAccepted {
		t.Fatal("the first two backups of a server fit")
	}
	if code := f.create(t, "edit-me"); code != http.StatusConflict {
		t.Fatalf("per-server limit (2): got %d, want 409", code)
	}
	if f.create(t, "other") != http.StatusAccepted {
		t.Fatal("another server has its own per-server allowance, until the org total")
	}
	if code := f.create(t, "other"); code != http.StatusConflict {
		t.Fatalf("org total (3): got %d, want 409", code)
	}

	// A failed backup does not count against the limits.
	bs := f.backups(t)
	bs[0].Status.Phase = gameserversv1alpha1.GameServerBackupPhaseFailed
	if err := f.srv.Client.Status().Update(t.Context(), &bs[0]); err != nil {
		t.Fatal(err)
	}
	if code := f.create(t, bs[0].Spec.GameServerRef.Name); code != http.StatusAccepted {
		t.Fatalf("a failed backup frees its slot: got %d", code)
	}
}

func TestConnectionBackupsUseTheServersBucketAndPrefixAndIgnoreLimits(t *testing.T) {
	f := newBackupFixture(t, true)
	f.putConnection(t, "mine", s3Connection("bucket-one", "bucket-two"))

	f.setTarget(t, "edit-me", map[string]string{"connection": "mine", "bucket": "bucket-two", "prefix": "worlds/x"})
	for range 5 { // well past the platform's per-server limit of 2
		if code := f.create(t, "edit-me"); code != http.StatusAccepted {
			t.Fatalf("connection backups are not limited: got %d", code)
		}
	}
	b := f.backups(t)[0]
	s3 := b.Spec.Destination.S3
	if s3.Bucket != "bucket-two" || s3.Endpoint != "https://s3.example.com" || s3.Prefix != "worlds/x" || s3.SecretRef.Name != "hatchery-s3-mine" {
		t.Fatalf("destination: %+v", s3)
	}
	if b.Labels[gameserversv1alpha1.BackupDestinationLabel] != "mine" {
		t.Fatalf("labels: %v", b.Labels)
	}
	if _, has := b.Annotations[gameserversv1alpha1.BackupExpiresAtAnnotation]; has {
		t.Fatal("the org's own storage has no platform retention")
	}

	// Another bucket of the same connection, with the prefix left empty: it defaults to the server's name.
	f.setTarget(t, "edit-me", map[string]string{"connection": "mine", "bucket": "bucket-one"})
	before := len(f.backups(t))
	if code := f.create(t, "edit-me"); code != http.StatusAccepted {
		t.Fatal("second bucket")
	}
	bs := f.backups(t)
	if len(bs) != before+1 {
		t.Fatalf("expected a new backup, got %d", len(bs))
	}
	found := false
	for _, x := range bs {
		if s := x.Spec.Destination.S3; s.Bucket == "bucket-one" && s.Prefix == "edit-me" {
			found = true
		}
	}
	if !found {
		t.Fatal("the same credentials can send a server's backups to another bucket, prefix defaulting to the server name")
	}
}

func TestConnectionsCannotBeRemovedWhileInUse(t *testing.T) {
	f := newBackupFixture(t, true)
	f.putConnection(t, "mine", s3Connection("bucket-one", "bucket-two"))
	f.setTarget(t, "edit-me", map[string]string{"connection": "mine", "bucket": "bucket-one"})
	del := func() int {
		return doRequest(t, f.srv, http.MethodDelete, orgURL("/backup-connections/mine"), f.admin, nil).Code
	}

	if code := del(); code != http.StatusConflict {
		t.Fatalf("a server uses it as its destination: got %d, want 409", code)
	}
	if code := f.putConnection(t, "mine", map[string]any{"endpoint": "https://s3.example.com", "buckets": []string{"bucket-two"}}); code != http.StatusConflict {
		t.Fatalf("removing a bucket a server uses: got %d, want 409", code)
	}
	if code := f.putConnection(t, "mine", map[string]any{"endpoint": "https://s3.example.com", "buckets": []string{"bucket-one"}}); code != http.StatusOK {
		t.Fatalf("removing an unused bucket: got %d, want 200", code)
	}

	if code := f.create(t, "edit-me"); code != http.StatusAccepted {
		t.Fatal("create")
	}
	f.setTarget(t, "edit-me", map[string]string{"connection": ""})
	if code := del(); code != http.StatusConflict {
		t.Fatalf("backups live in it: got %d, want 409", code)
	}
	name := f.backups(t)[0].Name
	if code := doRequest(t, f.srv, http.MethodDelete, orgURL("/gameservers/edit-me/backups/"+name), f.admin, nil).Code; code != http.StatusAccepted {
		t.Fatalf("delete backup: got %d", code)
	}
	if code := del(); code != http.StatusNoContent {
		t.Fatalf("unused: got %d, want 204", code)
	}
}

func TestListDeleteAndRestoreBackups(t *testing.T) {
	f := newBackupFixture(t, true)
	f.setTarget(t, "edit-me", map[string]string{"connection": "platform"})
	if f.create(t, "edit-me") != http.StatusAccepted {
		t.Fatal("create")
	}
	name := f.backups(t)[0].Name

	rec := doRequest(t, f.srv, http.MethodGet, orgURL("/gameservers/edit-me/backups"), f.member, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), name) {
		t.Fatalf("list: got %d %s", rec.Code, rec.Body.String())
	}
	var list backupListResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.Items) != 1 || list.Items[0].Destination != "platform" || list.Items[0].ExpiresAt == "" || list.Usage.Server != 1 || list.Usage.Org != 1 {
		t.Fatalf("list content: %+v", list)
	}

	restore := orgURL("/gameservers/edit-me/backups/" + name + "/restore")
	if rec := doRequest(t, f.srv, http.MethodPost, restore, f.admin, nil); rec.Code != http.StatusConflict {
		t.Fatalf("a backup that has not completed cannot be restored: got %d, want 409", rec.Code)
	}
	b := f.backups(t)[0]
	b.Status.Phase = gameserversv1alpha1.GameServerBackupPhaseCompleted
	if err := f.srv.Client.Status().Update(t.Context(), &b); err != nil {
		t.Fatal(err)
	}

	gs := getServer(t, f.srv, "edit-me")
	gs.Spec.State = gameserversv1alpha1.GameServerStateRunning
	if err := f.srv.Client.Update(t.Context(), &gs); err != nil {
		t.Fatal(err)
	}
	if rec := doRequest(t, f.srv, http.MethodPost, restore, f.admin, nil); rec.Code != http.StatusConflict {
		t.Fatalf("restoring over a running server: got %d, want 409: %s", rec.Code, rec.Body.String())
	}

	gs = getServer(t, f.srv, "edit-me")
	gs.Spec.State = gameserversv1alpha1.GameServerStateStopped
	if err := f.srv.Client.Update(t.Context(), &gs); err != nil {
		t.Fatal(err)
	}
	if rec := doRequest(t, f.srv, http.MethodPost, restore, f.member, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("member restoring: got %d, want 403", rec.Code)
	}
	if rec := doRequest(t, f.srv, http.MethodPost, restore, f.admin, nil); rec.Code != http.StatusAccepted {
		t.Fatalf("restore: got %d: %s", rec.Code, rec.Body.String())
	}
	var restores gameserversv1alpha1.GameServerRestoreList
	if err := f.srv.Client.List(t.Context(), &restores, client.InNamespace(testOrgNS())); err != nil || len(restores.Items) != 1 {
		t.Fatalf("expected one restore, got %d (%v)", len(restores.Items), err)
	}
	if r := restores.Items[0]; r.Spec.GameServerRef.Name != "edit-me" || r.Spec.BackupRef.Name != name {
		t.Fatalf("restore spec: %+v", r.Spec)
	}

	if rec := doRequest(t, f.srv, http.MethodDelete, orgURL("/gameservers/other-server/backups/"+name), f.admin, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("a backup of another server is not addressable here: got %d, want 404", rec.Code)
	}
	if rec := doRequest(t, f.srv, http.MethodDelete, orgURL("/gameservers/edit-me/backups/"+name), f.admin, nil); rec.Code != http.StatusAccepted {
		t.Fatalf("delete: got %d: %s", rec.Code, rec.Body.String())
	}
	if len(f.backups(t)) != 0 {
		t.Fatal("the backup must be deleted")
	}
}

func TestOrgQuotaCarriesTheBackupLimits(t *testing.T) {
	srv := newTestServer(t, tenantWithQuota(2, "10Gi"))
	platform := adminToken(t, srv)
	body := map[string]any{
		"cpu": "4", "memory": "8Gi", "storage": "10Gi", "maxGameServers": 2,
		"backups": map[string]int{"maxPerServer": 3, "maxPerOrg": 9, "retentionDays": 14},
	}
	if rec := doRequest(t, srv, http.MethodPatch, orgURL("/quota"), platform, body); rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	var tenant gameserversv1alpha1.Tenant
	if err := srv.Client.Get(t.Context(), client.ObjectKey{Name: testOrgSlug}, &tenant); err != nil {
		t.Fatal(err)
	}
	if b := tenant.Spec.Quota.Backups; b == nil || b.MaxPerServer != 3 || b.MaxPerOrg != 9 || b.RetentionDays != 14 {
		t.Fatalf("backups quota not stored: %+v", tenant.Spec.Quota.Backups)
	}
	body["backups"] = map[string]int{"maxPerServer": 3, "maxPerOrg": 9, "retentionDays": 0}
	if rec := doRequest(t, srv, http.MethodPatch, orgURL("/quota"), platform, body); rec.Code != http.StatusBadRequest {
		t.Fatalf("a retention below 1 day: got %d, want 400", rec.Code)
	}
}
