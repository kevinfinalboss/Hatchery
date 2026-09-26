package panelapi

import (
	"context"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

// DefaultAuditPruneInterval is how often old audit events are deleted.
const DefaultAuditPruneInterval = 24 * time.Hour

// AuditPruner deletes audit events past their retention: each organization's own
// (Tenant.spec.quota.auditRetentionDays) or DefaultDays for the others, for organizations that no
// longer exist and for platform events. Every panel replica may run it: the deletes are idempotent.
type AuditPruner struct {
	Client      client.Client
	DB          *paneldb.Store
	DefaultDays int // 0 keeps events without a retention of their own forever
	Interval    time.Duration
	Now         func() time.Time
}

// Run prunes once right away and then every Interval until ctx is done.
func (p *AuditPruner) Run(ctx context.Context) {
	interval := p.Interval
	if interval <= 0 {
		interval = DefaultAuditPruneInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if err := p.PruneOnce(ctx); err != nil {
			log.FromContext(ctx).WithName("audit-pruner").Error(err, "pruning the audit log")
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// PruneOnce runs one round. It stops at the first error; the next round tries again.
func (p *AuditPruner) PruneOnce(ctx context.Context) error {
	now := time.Now()
	if p.Now != nil {
		now = p.Now()
	}
	day := 24 * time.Hour
	var tenants gameserversv1alpha1.TenantList
	if err := p.Client.List(ctx, &tenants); err != nil {
		return err
	}
	var own []string
	var deleted int64
	for _, tn := range tenants.Items {
		d := tn.Spec.Quota.AuditRetentionDays
		if d == nil {
			continue
		}
		own = append(own, tn.Name)
		n, err := p.DB.PruneOrgAudit(ctx, tn.Name, now.Add(-time.Duration(*d)*day))
		if err != nil {
			return err
		}
		deleted += n
	}
	if p.DefaultDays > 0 {
		n, err := p.DB.PruneAuditExcept(ctx, now.Add(-time.Duration(p.DefaultDays)*day), own)
		if err != nil {
			return err
		}
		deleted += n
	}
	if deleted > 0 {
		log.FromContext(ctx).WithName("audit-pruner").Info("pruned the audit log", "deleted", deleted)
	}
	return nil
}
