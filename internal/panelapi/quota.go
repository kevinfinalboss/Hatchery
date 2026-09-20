package panelapi

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

func (s *Server) checkQuotaHeadroom(ctx context.Context, orgSlug, ns string, spec gameserversv1alpha1.GameServerSpec) error {
	var tenant gameserversv1alpha1.Tenant
	if err := s.Client.Get(ctx, client.ObjectKey{Name: orgSlug}, &tenant); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	var existing gameserversv1alpha1.GameServerList
	if err := s.Client.List(ctx, &existing, client.InNamespace(ns)); err != nil {
		return err
	}
	q := tenant.Spec.Quota

	if int32(len(existing.Items))+1 > q.MaxGameServers {
		return fmt.Errorf("quota exceeded: this organization may have at most %d game server(s)", q.MaxGameServers)
	}

	storage := resource.MustParse("0")
	cpu := resource.MustParse("0")
	mem := resource.MustParse("0")
	add := func(gs *gameserversv1alpha1.GameServerSpec) {
		if size, err := resource.ParseQuantity(gs.Storage.Size); err == nil {
			storage.Add(size)
		}
		cpu.Add(gs.Resources.Requests.Cpu().DeepCopy())
		mem.Add(gs.Resources.Requests.Memory().DeepCopy())
	}
	for i := range existing.Items {
		add(&existing.Items[i].Spec)
	}
	add(&spec)

	if storage.Cmp(q.Storage) > 0 {
		return fmt.Errorf("quota exceeded: storage would total %s of the %s allowed", storage.String(), q.Storage.String())
	}
	if cpu.Cmp(q.CPU) > 0 {
		return fmt.Errorf("quota exceeded: requested CPU would total %s of the %s allowed", cpu.String(), q.CPU.String())
	}
	if mem.Cmp(q.Memory) > 0 {
		return fmt.Errorf("quota exceeded: requested memory would total %s of the %s allowed", mem.String(), q.Memory.String())
	}
	return nil
}

// quotaRequest is the JSON shape of a tenant quota in the Panel API.
type quotaRequest struct {
	CPU            string `json:"cpu"`
	Memory         string `json:"memory"`
	Storage        string `json:"storage"`
	MaxGameServers int32  `json:"maxGameServers"`
}

// toQuota parses the quantities. Every field is required: an empty quantity
// would silently become "no capacity", which is never what a caller meant.
func (q quotaRequest) toQuota() (gameserversv1alpha1.TenantQuota, error) {
	var out gameserversv1alpha1.TenantQuota
	parse := func(field, v string) (resource.Quantity, error) {
		if v == "" {
			return resource.Quantity{}, fmt.Errorf("quota.%s is required", field)
		}
		qty, err := resource.ParseQuantity(v)
		if err != nil {
			return resource.Quantity{}, fmt.Errorf("quota.%s: %w", field, err)
		}
		return qty, nil
	}
	var err error
	if out.CPU, err = parse("cpu", q.CPU); err != nil {
		return out, err
	}
	if out.Memory, err = parse("memory", q.Memory); err != nil {
		return out, err
	}
	if out.Storage, err = parse("storage", q.Storage); err != nil {
		return out, err
	}
	if q.MaxGameServers < 0 {
		return out, fmt.Errorf("quota.maxGameServers must not be negative")
	}
	out.MaxGameServers = q.MaxGameServers
	return out, nil
}

func quotaResponseFrom(q gameserversv1alpha1.TenantQuota) quotaRequest {
	return quotaRequest{CPU: q.CPU.String(), Memory: q.Memory.String(), Storage: q.Storage.String(), MaxGameServers: q.MaxGameServers}
}
