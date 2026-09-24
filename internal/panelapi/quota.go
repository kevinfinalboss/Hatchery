package panelapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

// quotaTotals is what an org's existing servers already reserve, counted the same way the
// ResourceQuota does: CPU and memory by requests, storage by the PVC size.
type quotaTotals struct {
	CPU, Memory, Storage resource.Quantity
	GameServers          int32
}

func (t *quotaTotals) add(spec *gameserversv1alpha1.GameServerSpec) {
	if size, err := resource.ParseQuantity(spec.Storage.Size); err == nil {
		t.Storage.Add(size)
	}
	t.CPU.Add(spec.Resources.Requests.Cpu().DeepCopy())
	t.Memory.Add(spec.Resources.Requests.Memory().DeepCopy())
	t.GameServers++
}

// loadQuota reads the org's quota and what its servers (except `exclude`) already use. ok is false
// when the org has no Tenant CR, in which case the real ResourceQuota is the only authority.
func (s *Server) loadQuota(ctx context.Context, orgSlug, ns, exclude string) (q gameserversv1alpha1.TenantQuota, used quotaTotals, ok bool, err error) {
	var tenant gameserversv1alpha1.Tenant
	if err = s.Client.Get(ctx, client.ObjectKey{Name: orgSlug}, &tenant); err != nil {
		if apierrors.IsNotFound(err) {
			return q, used, false, nil
		}
		return q, used, false, err
	}
	var existing gameserversv1alpha1.GameServerList
	if err = s.Client.List(ctx, &existing, client.InNamespace(ns)); err != nil {
		return q, used, false, err
	}
	for i := range existing.Items {
		if existing.Items[i].Name != exclude {
			used.add(&existing.Items[i].Spec)
		}
	}
	return tenant.Spec.Quota, used, true, nil
}

// checkQuotaHeadroom refuses a new server that would push the org past its quota.
func (s *Server) checkQuotaHeadroom(ctx context.Context, orgSlug, ns string, spec gameserversv1alpha1.GameServerSpec) error {
	q, used, ok, err := s.loadQuota(ctx, orgSlug, ns, "")
	if err != nil || !ok {
		return err
	}
	if used.GameServers+1 > q.MaxGameServers {
		return fmt.Errorf("quota exceeded: this organization may have at most %d game server(s)", q.MaxGameServers)
	}
	used.add(&spec)
	return checkTotals(q, used)
}

// checkQuotaForUpdate is checkQuotaHeadroom for an edit: the server being edited is counted with its
// new spec instead of its old one, and the server-count limit does not apply.
func (s *Server) checkQuotaForUpdate(ctx context.Context, orgSlug, ns, name string, spec gameserversv1alpha1.GameServerSpec) error {
	q, used, ok, err := s.loadQuota(ctx, orgSlug, ns, name)
	if err != nil || !ok {
		return err
	}
	used.add(&spec)
	return checkTotals(q, used)
}

func checkTotals(q gameserversv1alpha1.TenantQuota, used quotaTotals) error {
	if used.Storage.Cmp(q.Storage) > 0 {
		return fmt.Errorf("quota exceeded: storage would total %s of the %s allowed", used.Storage.String(), q.Storage.String())
	}
	if used.CPU.Cmp(q.CPU) > 0 {
		return fmt.Errorf("quota exceeded: requested CPU would total %s of the %s allowed", used.CPU.String(), q.CPU.String())
	}
	if used.Memory.Cmp(q.Memory) > 0 {
		return fmt.Errorf("quota exceeded: requested memory would total %s of the %s allowed", used.Memory.String(), q.Memory.String())
	}
	return nil
}

// quotaUsageResponse is what the create/edit forms use to show how much an org has left.
type quotaUsageResponse struct {
	Limit quotaRequest `json:"limit"`
	Used  struct {
		CPU         string `json:"cpu"`
		Memory      string `json:"memory"`
		Storage     string `json:"storage"`
		GameServers int32  `json:"gameServers"`
	} `json:"used"`
}

func (s *Server) handleGetQuota(w http.ResponseWriter, r *http.Request) {
	acc := orgAccessFromContext(r.Context())
	q, used, ok, err := s.loadQuota(r.Context(), acc.Org.Slug, r.PathValue("namespace"), "")
	if err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "organization has no quota")
		return
	}
	var out quotaUsageResponse
	out.Limit = quotaResponseFrom(q)
	out.Used.CPU, out.Used.Memory, out.Used.Storage = used.CPU.String(), used.Memory.String(), used.Storage.String()
	out.Used.GameServers = used.GameServers
	writeJSON(w, http.StatusOK, out)
}

// quotaRequest is the JSON shape of a tenant quota in the Panel API.
type quotaRequest struct {
	CPU                  string        `json:"cpu"`
	Memory               string        `json:"memory"`
	Storage              string        `json:"storage"`
	MaxGameServers       int32         `json:"maxGameServers"`
	Backups              *backupLimits `json:"backups,omitempty"`
	ExtraImageRegistries []string      `json:"extraImageRegistries,omitempty"`
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
	if b := q.Backups; b != nil {
		if b.MaxPerServer < 0 || b.MaxPerOrg < 0 {
			return out, fmt.Errorf("quota.backups limits must not be negative")
		}
		if b.RetentionDays < 1 {
			return out, fmt.Errorf("quota.backups.retentionDays must be at least 1")
		}
		out.Backups = &gameserversv1alpha1.TenantBackupQuota{MaxPerServer: b.MaxPerServer, MaxPerOrg: b.MaxPerOrg, RetentionDays: b.RetentionDays}
	}
	for _, e := range q.ExtraImageRegistries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if strings.ContainsAny(e, " \t\n") || len(e) > 253 {
			return out, fmt.Errorf("quota.extraImageRegistries: %q is not a registry path", e)
		}
		out.ExtraImageRegistries = append(out.ExtraImageRegistries, e)
	}
	if len(out.ExtraImageRegistries) > 20 {
		return out, fmt.Errorf("quota.extraImageRegistries: at most 20 entries")
	}
	return out, nil
}

func quotaResponseFrom(q gameserversv1alpha1.TenantQuota) quotaRequest {
	out := quotaRequest{CPU: q.CPU.String(), Memory: q.Memory.String(), Storage: q.Storage.String(), MaxGameServers: q.MaxGameServers}
	if b := q.Backups; b != nil {
		out.Backups = &backupLimits{MaxPerServer: b.MaxPerServer, MaxPerOrg: b.MaxPerOrg, RetentionDays: b.RetentionDays}
	}
	out.ExtraImageRegistries = q.ExtraImageRegistries
	return out
}
