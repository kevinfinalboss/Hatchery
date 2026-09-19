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
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/client"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

// Eggs are read-only through the Panel API for now — authoring them is a
// cluster-admin, kubectl-apply activity (like the samples in
// config/samples/), not something the Panel exposes a write path for yet.

func (s *Server) handleListEggs(w http.ResponseWriter, r *http.Request) {
	var list gameserversv1alpha1.EggList
	var opts []client.ListOption
	if ns := r.URL.Query().Get("namespace"); ns != "" {
		opts = append(opts, client.InNamespace(ns))
	}
	if err := s.Client.List(r.Context(), &list, opts...); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGetEgg(w http.ResponseWriter, r *http.Request) {
	var egg gameserversv1alpha1.Egg
	key := client.ObjectKey{Namespace: r.PathValue("namespace"), Name: r.PathValue("name")}
	if err := s.Client.Get(r.Context(), key, &egg); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, egg)
}
