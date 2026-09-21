/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package panelapi

import (
	"encoding/json"
	"net/http"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

// maxEggBody caps an Egg request body. An Egg is a name, an image and a few
// scripts — 1 MiB is generous, and the cap stops a tenant from filling etcd
// through this endpoint.
const maxEggBody = 1 << 20

// eggEntry is an Egg together with where it lives, so the UI can tell catalog
// Eggs from the org's private ones.
type eggEntry struct {
	Name  string                       `json:"name"`
	Scope gameserversv1alpha1.EggScope `json:"scope"`
	Spec  gameserversv1alpha1.EggSpec  `json:"spec"`
}

type eggWriteRequest struct {
	Name string                      `json:"name"`
	Spec gameserversv1alpha1.EggSpec `json:"spec"`
}

func toEntries(list gameserversv1alpha1.EggList, scope gameserversv1alpha1.EggScope) []eggEntry {
	out := make([]eggEntry, 0, len(list.Items))
	for _, e := range list.Items {
		out = append(out, eggEntry{Name: e.Name, Scope: scope, Spec: e.Spec})
	}
	return out
}

// handleListOrgEggs returns the global catalog plus this org's own private
// Eggs — exactly what a GameServer in this org is allowed to reference. The
// private half is read from the org's derived namespace, so another org's
// Eggs cannot appear here.
func (s *Server) handleListOrgEggs(w http.ResponseWriter, r *http.Request) {
	var catalog, private gameserversv1alpha1.EggList
	if err := s.Client.List(r.Context(), &catalog, client.InNamespace(gameserversv1alpha1.CatalogNamespace)); err != nil && !apierrors.IsNotFound(err) {
		writeError(w, statusFor(err), err.Error())
		return
	}
	if err := s.Client.List(r.Context(), &private, client.InNamespace(r.PathValue("namespace"))); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, append(toEntries(catalog, gameserversv1alpha1.EggScopeCatalog), toEntries(private, gameserversv1alpha1.EggScopeNamespace)...))
}

// handleGetOrgEgg reads one Egg. {scope} is "catalog" or "private"; private
// always means this org's namespace.
func (s *Server) handleGetOrgEgg(w http.ResponseWriter, r *http.Request) {
	var ns string
	var scope gameserversv1alpha1.EggScope
	switch r.PathValue("scope") {
	case "catalog":
		ns, scope = gameserversv1alpha1.CatalogNamespace, gameserversv1alpha1.EggScopeCatalog
	case "private":
		ns, scope = r.PathValue("namespace"), gameserversv1alpha1.EggScopeNamespace
	default:
		writeError(w, http.StatusBadRequest, `scope must be "catalog" or "private"`)
		return
	}
	var egg gameserversv1alpha1.Egg
	if err := s.Client.Get(r.Context(), client.ObjectKey{Namespace: ns, Name: r.PathValue("name")}, &egg); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, eggEntry{Name: egg.Name, Scope: scope, Spec: egg.Spec})
}

func (s *Server) handleListCatalogEggs(w http.ResponseWriter, r *http.Request) {
	var list gameserversv1alpha1.EggList
	if err := s.Client.List(r.Context(), &list, client.InNamespace(gameserversv1alpha1.CatalogNamespace)); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toEntries(list, gameserversv1alpha1.EggScopeCatalog))
}

func decodeEggRequest(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxEggBody)).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

// createEgg, updateEgg and deleteEgg are shared by the org (private) and
// catalog routes; only the namespace and the audit identity differ.

func (s *Server) createEgg(w http.ResponseWriter, r *http.Request, ns, orgSlug, action string) {
	var req eggWriteRequest
	if !decodeEggRequest(w, r, &req) {
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if msgs := req.Spec.Validate(); len(msgs) > 0 {
		writeError(w, http.StatusUnprocessableEntity, strings.Join(msgs, "; "))
		return
	}
	egg := &gameserversv1alpha1.Egg{ObjectMeta: metav1.ObjectMeta{Name: req.Name, Namespace: ns}, Spec: req.Spec}
	if err := s.Client.Create(r.Context(), egg); err != nil {
		s.auditEvent(r, orgSlug, action, "egg", req.Name, "failed", nil)
		writeError(w, statusFor(err), err.Error())
		return
	}
	s.auditEvent(r, orgSlug, action, "egg", req.Name, "success", nil)
	writeJSON(w, http.StatusCreated, eggEntry{Name: egg.Name, Spec: egg.Spec})
}

func (s *Server) updateEgg(w http.ResponseWriter, r *http.Request, ns, orgSlug, action string) {
	var req eggWriteRequest
	if !decodeEggRequest(w, r, &req) {
		return
	}
	if msgs := req.Spec.Validate(); len(msgs) > 0 {
		writeError(w, http.StatusUnprocessableEntity, strings.Join(msgs, "; "))
		return
	}
	name := r.PathValue("name")
	var egg gameserversv1alpha1.Egg
	if err := s.Client.Get(r.Context(), client.ObjectKey{Namespace: ns, Name: name}, &egg); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	egg.Spec = req.Spec
	if err := s.Client.Update(r.Context(), &egg); err != nil {
		s.auditEvent(r, orgSlug, action, "egg", name, "failed", nil)
		writeError(w, statusFor(err), err.Error())
		return
	}
	s.auditEvent(r, orgSlug, action, "egg", name, "success", nil)
	writeJSON(w, http.StatusOK, eggEntry{Name: egg.Name, Spec: egg.Spec})
}

func (s *Server) deleteEgg(w http.ResponseWriter, r *http.Request, ns, orgSlug, action string) {
	name := r.PathValue("name")
	if err := s.Client.Delete(r.Context(), &gameserversv1alpha1.Egg{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}); err != nil {
		s.auditEvent(r, orgSlug, action, "egg", name, "failed", nil)
		writeError(w, statusFor(err), err.Error())
		return
	}
	s.auditEvent(r, orgSlug, action, "egg", name, "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCreateOrgEgg(w http.ResponseWriter, r *http.Request) {
	s.createEgg(w, r, r.PathValue("namespace"), orgAccessFromContext(r.Context()).Org.Slug, "egg.create")
}

func (s *Server) handleUpdateOrgEgg(w http.ResponseWriter, r *http.Request) {
	s.updateEgg(w, r, r.PathValue("namespace"), orgAccessFromContext(r.Context()).Org.Slug, "egg.update")
}

// handleDeleteOrgEgg refuses to delete a private Egg some GameServer of the
// org still uses: that server would go Failed on its next reconcile.
func (s *Server) handleDeleteOrgEgg(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("namespace")
	var servers gameserversv1alpha1.GameServerList
	if err := s.Client.List(r.Context(), &servers, client.InNamespace(ns)); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	for _, gs := range servers.Items {
		if gs.Spec.EggRef.Name == r.PathValue("name") && gs.Spec.EggRef.Scope != gameserversv1alpha1.EggScopeCatalog {
			writeError(w, http.StatusConflict, "egg is still used by game server "+gs.Name)
			return
		}
	}
	s.deleteEgg(w, r, ns, orgAccessFromContext(r.Context()).Org.Slug, "egg.delete")
}

func (s *Server) handleCreateCatalogEgg(w http.ResponseWriter, r *http.Request) {
	s.createEgg(w, r, gameserversv1alpha1.CatalogNamespace, "", "catalog.egg.create")
}

func (s *Server) handleUpdateCatalogEgg(w http.ResponseWriter, r *http.Request) {
	s.updateEgg(w, r, gameserversv1alpha1.CatalogNamespace, "", "catalog.egg.update")
}

// handleDeleteCatalogEgg does not check for users of the Egg: the Panel has
// no cluster-wide view of GameServers by design (see the RBAC in
// config/rbac/panel_cluster_role.yaml). Dependents go Failed on their next
// reconcile; running Pods are untouched.
func (s *Server) handleDeleteCatalogEgg(w http.ResponseWriter, r *http.Request) {
	s.deleteEgg(w, r, gameserversv1alpha1.CatalogNamespace, "", "catalog.egg.delete")
}
