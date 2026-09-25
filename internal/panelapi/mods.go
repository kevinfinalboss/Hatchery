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
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/modsource"
)

// modTargetInfo is the server a mods route acts on, with its Egg's mods block.
type modTargetInfo struct {
	GS   *gameserversv1alpha1.GameServer
	Egg  *gameserversv1alpha1.Egg
	Mods gameserversv1alpha1.EggMods
}

// modTarget loads the GameServer and its Egg, or writes the error: an Egg without a mods block
// means the installer does not exist for this server (404).
func (s *Server) modTarget(w http.ResponseWriter, r *http.Request) (*modTargetInfo, bool) {
	gs, ok := s.targetGameServer(w, r)
	if !ok {
		return nil, false
	}
	var egg gameserversv1alpha1.Egg
	if err := s.Client.Get(r.Context(), client.ObjectKey{Namespace: gs.EggNamespace(), Name: gs.Spec.EggRef.Name}, &egg); err != nil {
		if apierrors.IsNotFound(err) {
			writeError(w, http.StatusNotFound, "this server's egg does not support mods")
			return nil, false
		}
		writeError(w, statusFor(err), err.Error())
		return nil, false
	}
	if egg.Spec.Mods == nil {
		writeError(w, http.StatusNotFound, "this server's egg does not support mods")
		return nil, false
	}
	return &modTargetInfo{GS: gs, Egg: &egg, Mods: *egg.Spec.Mods}, true
}

func concreteVersion(v string) bool {
	v = strings.TrimSpace(v)
	return v != "" && !strings.EqualFold(v, "latest") && !strings.EqualFold(v, "snapshot")
}

// variableValue is the server's override of an Egg variable, else the Egg's default.
func variableValue(gs *gameserversv1alpha1.GameServer, egg *gameserversv1alpha1.Egg, name string) string {
	for _, v := range gs.Spec.Variables {
		if v.Name == name {
			return v.Value
		}
	}
	for _, v := range egg.Spec.Variables {
		if v.Name == name {
			return v.Default
		}
	}
	return ""
}

// gameVersion is the running game version: the variable when it is concrete, else the file the
// install wrote with the version it downloaded, else "" (unknown — the UI asks the user).
func (s *Server) gameVersion(ctx context.Context, t *modTargetInfo) (string, error) {
	gv := t.Mods.GameVersion
	if gv.Variable != "" {
		if v := variableValue(t.GS, t.Egg, gv.Variable); concreteVersion(v) {
			return strings.TrimSpace(v), nil
		}
	}
	if gv.File == "" {
		return "", nil
	}
	conn, err := s.openFileSFTPClient(ctx, t.GS)
	if err != nil {
		// The files are unreachable (the server is starting or restarting): use the version
		// read last time rather than failing the whole mods tab.
		return s.rememberedGameVersion(ctx, t), nil
	}
	defer conn.Close()
	return s.readAndRememberGameVersion(ctx, conn, t), nil
}

func (s *Server) rememberedGameVersion(ctx context.Context, t *modTargetInfo) string {
	if s.GameVersions == nil {
		return ""
	}
	v, _ := s.GameVersions.Get(ctx, t.GS.Namespace, t.GS.Name)
	return v
}

// readAndRememberGameVersion reads the version file through an open connection and remembers
// it for when the files are unreachable.
func (s *Server) readAndRememberGameVersion(ctx context.Context, conn *sftpConn, t *modTargetInfo) string {
	v := readGameVersionFile(conn, t.Mods.GameVersion.File)
	if v != "" && s.GameVersions != nil {
		_ = s.GameVersions.Set(ctx, t.GS.Namespace, t.GS.Name, v) // best effort
	}
	return v
}

// writeFilesUnreachable answers a mods write when the server's files cannot be reached.
func writeFilesUnreachable(w http.ResponseWriter, err error) {
	writeError(w, http.StatusBadGateway, "the server's files are not reachable right now (it may be starting or restarting): try again in a moment ("+err.Error()+")")
}

func readGameVersionFile(conn *sftpConn, file string) string {
	f, err := conn.Open(path.Join("/", file))
	if err != nil {
		return "" // not written (yet): unknown, not an error
	}
	defer f.Close()
	b, _ := io.ReadAll(io.LimitReader(f, 64))
	if v := strings.TrimSpace(string(b)); concreteVersion(v) {
		return v
	}
	return ""
}

// modFilter builds the source filter; a gameVersion query parameter (chosen in the UI when the
// version is unknown) overrides the detected one.
func modFilter(r *http.Request, t *modTargetInfo, detected string) modsource.Filter {
	gv := detected
	if q := strings.TrimSpace(r.URL.Query().Get("gameVersion")); q != "" {
		gv = q
	}
	return modsource.Filter{Kind: modsource.Kind(t.Mods.Kind), Loaders: t.Mods.Loaders, GameVersion: gv}
}

// modSourceNames lists the configured sources in a fixed order (Modrinth first).
func (s *Server) modSourceNames() []string {
	names := make([]string, 0, len(s.ModSources))
	for n := range s.ModSources {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		return names[i] == "modrinth" || (names[j] != "modrinth" && names[i] < names[j])
	})
	return names
}

func (s *Server) modSource(w http.ResponseWriter, name string) (modsource.Source, bool) {
	src, ok := s.ModSources[name]
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown or unconfigured mod source "+strconv.Quote(name))
	}
	return src, ok
}

// writeSourceError maps catalog failures: down → 502, rate limit → 429 with Retry-After.
func writeSourceError(w http.ResponseWriter, err error) {
	var rl *modsource.RateLimitedError
	switch {
	case errors.As(err, &rl):
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(rl.RetryAfter.Seconds()))))
		writeError(w, http.StatusTooManyRequests, err.Error())
	case errors.Is(err, modsource.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, modsource.ErrUnavailable):
		writeError(w, http.StatusBadGateway, err.Error())
	default:
		writeError(w, http.StatusBadGateway, err.Error())
	}
}

type modsContextResponse struct {
	Kind        gameserversv1alpha1.EggModsKind `json:"kind"`
	Loaders     []string                        `json:"loaders"`
	Directory   string                          `json:"directory"`
	GameVersion *string                         `json:"gameVersion"`
	Sources     []string                        `json:"sources"`
}

func (s *Server) handleModsContext(w http.ResponseWriter, r *http.Request) {
	t, ok := s.modTarget(w, r)
	if !ok {
		return
	}
	gv, err := s.gameVersion(r.Context(), t)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	resp := modsContextResponse{Kind: t.Mods.Kind, Loaders: t.Mods.Loaders, Directory: t.Mods.Directory, Sources: s.modSourceNames()}
	if gv != "" {
		resp.GameVersion = &gv
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleSearchMods(w http.ResponseWriter, r *http.Request) {
	t, ok := s.modTarget(w, r)
	if !ok {
		return
	}
	src, ok := s.modSource(w, r.URL.Query().Get("source"))
	if !ok {
		return
	}
	gv, err := s.gameVersion(r.Context(), t)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 0 {
		page = 0
	}
	res, err := src.Search(r.Context(), modsource.SearchQuery{Filter: modFilter(r, t, gv), Query: r.URL.Query().Get("q"), Page: page})
	if err != nil {
		writeSourceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleModVersions(w http.ResponseWriter, r *http.Request) {
	t, ok := s.modTarget(w, r)
	if !ok {
		return
	}
	src, ok := s.modSource(w, r.PathValue("source"))
	if !ok {
		return
	}
	gv, err := s.gameVersion(r.Context(), t)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	vs, err := src.Versions(r.Context(), r.PathValue("projectId"), modFilter(r, t, gv))
	if err != nil {
		writeSourceError(w, err)
		return
	}
	if vs == nil {
		vs = []modsource.Version{}
	}
	writeJSON(w, http.StatusOK, vs)
}
