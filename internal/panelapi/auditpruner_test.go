package panelapi

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

func TestAuditPrunerUsesEachOrgsRetention(t *testing.T) {
	days := int32(30)
	custom := &gameserversv1alpha1.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "custom"},
		Spec: gameserversv1alpha1.TenantSpec{Quota: gameserversv1alpha1.TenantQuota{AuditRetentionDays: &days}}}
	plain := &gameserversv1alpha1.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "plain"}}
	srv := newTestServer(t, custom, plain)
	ctx := context.Background()
	for _, org := range []string{"custom", "plain", ""} {
		if err := srv.DB.RecordAudit(ctx, paneldb.AuditEvent{OrgSlug: org, ActorUsername: "u", Action: "x", Outcome: "success"}); err != nil {
			t.Fatal(err)
		}
	}
	count := func(org string) int {
		ev, err := srv.DB.ListAudit(ctx, org, 100, 0)
		if err != nil {
			t.Fatal(err)
		}
		return len(ev)
	}
	p := &AuditPruner{Client: srv.Client, DB: srv.DB, DefaultDays: 365}

	p.Now = func() time.Time { return time.Now().Add(40 * 24 * time.Hour) } // events are 40 days old
	if err := p.PruneOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if count("custom") != 0 || count("plain") != 1 || count("") != 1 {
		t.Fatalf("after 40 days: custom=%d plain=%d platform=%d; want 0 1 1", count("custom"), count("plain"), count(""))
	}

	p.Now = func() time.Time { return time.Now().Add(400 * 24 * time.Hour) }
	if err := p.PruneOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if count("plain") != 0 || count("") != 0 {
		t.Fatalf("after 400 days: plain=%d platform=%d; want 0 0", count("plain"), count(""))
	}
}

func TestAuditPrunerZeroKeepsTheDefaultForever(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	if err := srv.DB.RecordAudit(ctx, paneldb.AuditEvent{ActorUsername: "u", Action: "x", Outcome: "success"}); err != nil {
		t.Fatal(err)
	}
	p := &AuditPruner{Client: srv.Client, DB: srv.DB, DefaultDays: 0, Now: func() time.Time { return time.Now().Add(9000 * 24 * time.Hour) }}
	if err := p.PruneOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if ev, _ := srv.DB.ListAudit(ctx, "", 10, 0); len(ev) != 1 {
		t.Fatalf("DefaultDays=0 must not prune, left %d", len(ev))
	}
}
