package backupplan

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

func newFake(t *testing.T, objs ...client.Object) client.Client {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = v1alpha1.AddToScheme(s)
	return fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).WithStatusSubresource(&v1alpha1.GameServerBackup{}).Build()
}

func server(target *v1alpha1.BackupTarget) *v1alpha1.GameServer {
	return &v1alpha1.GameServer{ObjectMeta: metav1.ObjectMeta{Name: "mc", Namespace: "hatchery-acme"},
		Spec: v1alpha1.GameServerSpec{BackupTarget: target}}
}

func TestPlanPlatformAppliesLimitsAndExpiry(t *testing.T) {
	tenant := &v1alpha1.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "acme"}, Spec: v1alpha1.TenantSpec{Quota: v1alpha1.TenantQuota{
		CPU: resource.MustParse("1"), Memory: resource.MustParse("1Gi"), Storage: resource.MustParse("1Gi"),
		Backups: &v1alpha1.TenantBackupQuota{MaxPerServer: 1, MaxPerOrg: 5, RetentionDays: 7},
	}}}
	platformSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "plat", Namespace: "hatchery-system"},
		Data: map[string][]byte{"access-key": []byte("a"), "secret-key": []byte("s")}}
	c := newFake(t, tenant, platformSecret)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	p := &Planner{Client: c, Now: func() time.Time { return now },
		Platform: Platform{Endpoint: "s3.example", Bucket: "plat", SecretNamespace: "hatchery-system", SecretName: "plat"}}

	bkp, err := p.Plan(context.Background(), server(&v1alpha1.BackupTarget{Connection: PlatformConnection}), "acme")
	if err != nil {
		t.Fatal(err)
	}
	if bkp.Spec.Destination.S3.Prefix != "acme/mc" || bkp.Annotations[v1alpha1.BackupExpiresAtAnnotation] != "2026-09-30T12:00:00Z" {
		t.Fatalf("got %+v %v", bkp.Spec.Destination.S3, bkp.Annotations)
	}
	if err := p.Create(context.Background(), bkp); err != nil {
		t.Fatal(err)
	}
	// The per-server limit (1) is now reached.
	_, err = p.Plan(context.Background(), server(&v1alpha1.BackupTarget{Connection: PlatformConnection}), "acme")
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("want a ConflictError at the limit, got %v", err)
	}
}

func TestPlanWithoutTargetIsAConflict(t *testing.T) {
	p := &Planner{Client: newFake(t), Now: time.Now}
	_, err := p.Plan(context.Background(), server(nil), "acme")
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("got %v", err)
	}
}
