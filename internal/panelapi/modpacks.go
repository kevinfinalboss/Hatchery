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
	"strconv"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/modsource"
)

// Variables of the modpack Egg (config/samples/gameservers_v1alpha1_egg_modpack.yaml).
const (
	modpackSourceVar    = "HATCHERY_PACK_SOURCE"
	modpackVar          = "HATCHERY_PACK"
	modpackProjectIDVar = "HATCHERY_PACK_PROJECT_ID"
	modpackVersionVar   = "HATCHERY_PACK_VERSION"
)

// javaImageFor picks the modpack Egg image for a Minecraft version: 26.x needs Java 25, 1.20.5+
// Java 21, 1.17+ Java 17, older Java 8. Unknown versions get the newest.
func javaImageFor(gameVersion string) string {
	parts := strings.Split(gameVersion, ".")
	num := func(i int) int {
		if i >= len(parts) {
			return 0
		}
		n, _ := strconv.Atoi(parts[i])
		return n
	}
	switch {
	case gameVersion == "" || num(0) >= 26:
		return "Java 25"
	case num(0) == 1 && (num(1) > 20 || (num(1) == 20 && num(2) >= 5)):
		return "Java 21"
	case num(0) == 1 && num(1) >= 17:
		return "Java 17"
	case num(0) == 1:
		return "Java 8"
	}
	return "Java 25"
}

// modpackVersion is a pack version plus what the create form needs: its Minecraft version and the
// image that runs it.
type modpackVersion struct {
	modsource.Version
	GameVersion string `json:"gameVersion"`
	ImageName   string `json:"imageName"`
}

func toModpackVersions(vs []modsource.Version) []modpackVersion {
	out := make([]modpackVersion, 0, len(vs))
	for _, v := range vs {
		gv := ""
		if len(v.GameVersions) > 0 {
			gv = v.GameVersions[0]
		}
		out = append(out, modpackVersion{Version: v, GameVersion: gv, ImageName: javaImageFor(gv)})
	}
	return out
}

var modpackFilter = modsource.Filter{Kind: modsource.KindModpack}

func (s *Server) handleSearchModpacks(w http.ResponseWriter, r *http.Request) {
	src, ok := s.modSource(w, r.URL.Query().Get("source"))
	if !ok {
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 0 {
		page = 0
	}
	res, err := src.Search(r.Context(), modsource.SearchQuery{Filter: modpackFilter, Query: r.URL.Query().Get("q"), Page: page})
	if err != nil {
		writeSourceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleModpackVersions(w http.ResponseWriter, r *http.Request) {
	src, ok := s.modSource(w, r.PathValue("source"))
	if !ok {
		return
	}
	vs, err := src.Versions(r.Context(), r.PathValue("projectId"), modpackFilter)
	if err != nil {
		writeSourceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toModpackVersions(vs))
}

// modpackTarget loads a server whose Egg is a modpack Egg, with its pack coordinates.
type modpackTarget struct {
	gs        *gameserversv1alpha1.GameServer
	egg       *gameserversv1alpha1.Egg
	source    string
	projectID string
	versionID string
}

func (s *Server) modpackTargetOf(w http.ResponseWriter, r *http.Request) (*modpackTarget, bool) {
	gs, ok := s.targetGameServer(w, r)
	if !ok {
		return nil, false
	}
	var egg gameserversv1alpha1.Egg
	if err := s.Client.Get(r.Context(), client.ObjectKey{Namespace: gs.EggNamespace(), Name: gs.Spec.EggRef.Name}, &egg); err != nil || !egg.IsModpack() {
		if err != nil && !apierrors.IsNotFound(err) {
			writeError(w, statusFor(err), err.Error())
			return nil, false
		}
		writeError(w, http.StatusNotFound, "this server is not built from a modpack")
		return nil, false
	}
	t := &modpackTarget{gs: gs, egg: &egg,
		source:    variableValue(gs, &egg, modpackSourceVar),
		projectID: variableValue(gs, &egg, modpackProjectIDVar),
		versionID: variableValue(gs, &egg, modpackVersionVar),
	}
	if t.projectID == "" {
		t.projectID = variableValue(gs, &egg, modpackVar)
	}
	return t, true
}

type modpackStatus struct {
	Source          string          `json:"source"`
	ProjectID       string          `json:"projectId"`
	Version         *modpackVersion `json:"version"`
	Latest          *modpackVersion `json:"latest"`
	UpdateAvailable bool            `json:"updateAvailable"`
}

func (s *Server) handleModpackStatus(w http.ResponseWriter, r *http.Request) {
	t, ok := s.modpackTargetOf(w, r)
	if !ok {
		return
	}
	src, ok := s.modSource(w, t.source)
	if !ok {
		return
	}
	vs, err := src.Versions(r.Context(), t.projectID, modpackFilter)
	if err != nil {
		writeSourceError(w, err)
		return
	}
	all := toModpackVersions(vs)
	st := modpackStatus{Source: t.source, ProjectID: t.projectID}
	if len(all) > 0 {
		st.Latest = &all[0]
	}
	for i := range all {
		if all[i].ID == t.versionID {
			st.Version = &all[i]
		}
	}
	if st.Latest != nil {
		if st.Version != nil {
			st.UpdateAvailable = modsource.IsNewer(st.Latest.Version, st.Version.Version)
		} else {
			st.UpdateAvailable = st.Latest.ID != t.versionID
		}
	}
	writeJSON(w, http.StatusOK, st)
}

type modpackUpdateRequest struct {
	VersionID string `json:"versionId"`
}

// handleUpdateModpack pins another pack version (and the image its Minecraft version needs) and
// restarts a running server; the itzg image swaps the pack and cleans up the old files on start.
func (s *Server) handleUpdateModpack(w http.ResponseWriter, r *http.Request) {
	t, ok := s.modpackTargetOf(w, r)
	if !ok {
		return
	}
	var req modpackUpdateRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&req); err != nil || req.VersionID == "" {
		writeError(w, http.StatusBadRequest, `body must be {"versionId": "..."}`)
		return
	}
	src, ok := s.modSource(w, t.source)
	if !ok {
		return
	}
	vs, err := src.Versions(r.Context(), t.projectID, modpackFilter)
	if err != nil {
		writeSourceError(w, err)
		return
	}
	var target *modpackVersion
	for _, v := range toModpackVersions(vs) {
		if v.ID == req.VersionID {
			v := v
			target = &v
		}
	}
	if target == nil {
		writeError(w, http.StatusUnprocessableEntity, "this modpack has no version "+strconv.Quote(req.VersionID))
		return
	}
	_, hasImage := t.egg.ResolveImage(target.ImageName)
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var gs gameserversv1alpha1.GameServer
		if err := s.Client.Get(r.Context(), client.ObjectKeyFromObject(t.gs), &gs); err != nil {
			return err
		}
		set := false
		for i := range gs.Spec.Variables {
			if gs.Spec.Variables[i].Name == modpackVersionVar {
				gs.Spec.Variables[i].Value, set = target.ID, true
			}
		}
		if !set {
			gs.Spec.Variables = append(gs.Spec.Variables, gameserversv1alpha1.GameServerVariable{Name: modpackVersionVar, Value: target.ID})
		}
		if hasImage {
			gs.Spec.ImageName = target.ImageName
		}
		if gs.Spec.State == gameserversv1alpha1.GameServerStateRunning {
			if gs.Annotations == nil {
				gs.Annotations = map[string]string{}
			}
			gs.Annotations[gameserversv1alpha1.RestartAnnotation] = time.Now().UTC().Format(time.RFC3339Nano)
		}
		return s.Client.Update(r.Context(), &gs)
	})
	if err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	s.auditMod(r, "modpack.update", map[string]string{"source": t.source, "project": t.projectID, "from": t.versionID, "to": target.ID})
	writeJSON(w, http.StatusOK, target)
}
