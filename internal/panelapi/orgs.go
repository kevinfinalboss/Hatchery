package panelapi

import (
	"encoding/json"
	"errors"
	"net/http"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

type orgSummary struct {
	Slug string       `json:"slug"`
	Name string       `json:"name"`
	Role paneldb.Role `json:"role"`
}

type orgDetail struct {
	Slug      string        `json:"slug"`
	Name      string        `json:"name"`
	Role      paneldb.Role  `json:"role"`
	Namespace string        `json:"namespace"`
	Phase     string        `json:"phase"`
	Ready     bool          `json:"ready"`
	Quota     *quotaRequest `json:"quota,omitempty"`
}

// handleListOrgs returns the caller's orgs with their role in each. A
// platform admin sees every org, with the effective role owner.
func (s *Server) handleListOrgs(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	out := []orgSummary{}
	if user.IsAdmin {
		orgs, err := s.DB.ListAllOrgs(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, o := range orgs {
			out = append(out, orgSummary{Slug: o.Slug, Name: o.Name, Role: paneldb.RoleOwner})
		}
	} else {
		orgs, err := s.DB.ListOrgsForUser(r.Context(), user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, o := range orgs {
			out = append(out, orgSummary{Slug: o.Slug, Name: o.Name, Role: o.Role})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

type createOrgRequest struct {
	Slug          string       `json:"slug"`
	Name          string       `json:"name"`
	OwnerUsername string       `json:"ownerUsername"`
	Quota         quotaRequest `json:"quota"`
}

// handleCreateOrg creates the org row (with its first owner) and then the
// Tenant CR the operator turns into a namespace. If the CR cannot be created
// the row is removed again, so a half-created org never lingers.
func (s *Server) handleCreateOrg(w http.ResponseWriter, r *http.Request) {
	var req createOrgRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if !paneldb.ValidSlug(req.Slug) {
		writeError(w, http.StatusBadRequest, "slug must be a lowercase DNS label of at most 32 characters and cannot be 'system' or 'catalog'")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	quota, err := req.Quota.toQuota()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	owner, err := s.DB.GetUserByUsername(r.Context(), req.OwnerUsername)
	if err != nil {
		if errors.Is(err, paneldb.ErrNotFound) {
			writeError(w, http.StatusNotFound, "owner user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	org, err := s.DB.CreateOrg(r.Context(), req.Slug, req.Name, owner.ID)
	if err != nil {
		if errors.Is(err, paneldb.ErrAlreadyExists) {
			writeError(w, http.StatusConflict, "an organization with this slug already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	tenant := &gameserversv1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: org.Slug},
		Spec:       gameserversv1alpha1.TenantSpec{DisplayName: org.Name, Quota: quota},
	}
	if err := s.Client.Create(r.Context(), tenant); err != nil {
		_ = s.DB.DeleteOrg(r.Context(), org.Slug) // compensate: no row without a Tenant
		s.auditEvent(r, org.Slug, "org.create", "org", org.Slug, "failed", nil)
		writeError(w, statusFor(err), "creating tenant: "+err.Error())
		return
	}

	s.auditEvent(r, org.Slug, "org.create", "org", org.Slug, "success", map[string]string{"owner": owner.Username})
	writeJSON(w, http.StatusCreated, orgSummary{Slug: org.Slug, Name: org.Name, Role: paneldb.RoleOwner})
}

// handleGetOrg returns the org with the live state of its Tenant, so the UI
// can show "provisioning" until the operator has the namespace ready.
func (s *Server) handleGetOrg(w http.ResponseWriter, r *http.Request) {
	acc := orgAccessFromContext(r.Context())
	d := orgDetail{
		Slug: acc.Org.Slug, Name: acc.Org.Name, Role: acc.Role,
		Namespace: gameserversv1alpha1.TenantNamespace(acc.Org.Slug), Phase: "Missing",
	}
	var tenant gameserversv1alpha1.Tenant
	switch err := s.Client.Get(r.Context(), client.ObjectKey{Name: acc.Org.Slug}, &tenant); {
	case err == nil:
		d.Phase = string(tenant.Status.Phase)
		if d.Phase == "" {
			d.Phase = string(gameserversv1alpha1.TenantPhasePending)
		}
		d.Ready = tenant.Status.Phase == gameserversv1alpha1.TenantPhaseActive
		q := quotaResponseFrom(tenant.Spec.Quota)
		d.Quota = &q
	case !apierrors.IsNotFound(err):
		writeError(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// handleDeleteOrg refuses while anything is left in the org's namespace, then
// deletes the Tenant and the row. The Tenant's own finalizer independently
// waits for the namespace to be empty, so this check is for a clear error.
func (s *Server) handleDeleteOrg(w http.ResponseWriter, r *http.Request) {
	acc := orgAccessFromContext(r.Context())
	ns := r.PathValue("namespace")

	var servers gameserversv1alpha1.GameServerList
	var backups gameserversv1alpha1.GameServerBackupList
	var restores gameserversv1alpha1.GameServerRestoreList
	for _, list := range []client.ObjectList{&servers, &backups, &restores} {
		if err := s.Client.List(r.Context(), list, client.InNamespace(ns)); err != nil && !apierrors.IsNotFound(err) {
			writeError(w, statusFor(err), err.Error())
			return
		}
	}
	if n := len(servers.Items) + len(backups.Items) + len(restores.Items); n > 0 {
		s.auditEvent(r, acc.Org.Slug, "org.delete", "org", acc.Org.Slug, "failed", nil)
		writeError(w, http.StatusConflict, "organization still has game servers, backups or restores: delete them first")
		return
	}

	tenant := &gameserversv1alpha1.Tenant{ObjectMeta: metav1.ObjectMeta{Name: acc.Org.Slug}}
	if err := s.Client.Delete(r.Context(), tenant); err != nil && !apierrors.IsNotFound(err) {
		writeError(w, statusFor(err), err.Error())
		return
	}
	if err := s.DB.DeleteOrg(r.Context(), acc.Org.Slug); err != nil && !errors.Is(err, paneldb.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditEvent(r, acc.Org.Slug, "org.delete", "org", acc.Org.Slug, "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

// handleUpdateQuota changes the Tenant's quota. Platform admin only (the
// route wraps it in requireAdmin): quota is what the platform sells.
func (s *Server) handleUpdateQuota(w http.ResponseWriter, r *http.Request) {
	acc := orgAccessFromContext(r.Context())
	var req quotaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	quota, err := req.toQuota()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var tenant gameserversv1alpha1.Tenant
	if err := s.Client.Get(r.Context(), client.ObjectKey{Name: acc.Org.Slug}, &tenant); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	tenant.Spec.Quota = quota
	if err := s.Client.Update(r.Context(), &tenant); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	s.auditEvent(r, acc.Org.Slug, "org.quota", "org", acc.Org.Slug, "success", nil)
	q := quotaResponseFrom(quota)
	writeJSON(w, http.StatusOK, q)
}
