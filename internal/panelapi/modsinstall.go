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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"slices"
	"sort"
	"strings"
	"time"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/modsource"
	"github.com/kevinfinalboss/Hatchery/internal/panelcache"
)

// modMaxBytes caps one downloaded mod/plugin file.
const modMaxBytes int64 = 256 << 20

var (
	errModTooLarge     = errors.New("mod file is larger than the 256 MB limit")
	errModHashMismatch = errors.New("downloaded file does not match the hash published by the source")
)

func (s *Server) modMaxBytes() int64 {
	if s.modMaxBytesOverride > 0 {
		return s.modMaxBytesOverride
	}
	return modMaxBytes
}

func (s *Server) modDownloader() *http.Client {
	if s.modHTTP != nil {
		return s.modHTTP
	}
	return &http.Client{Timeout: 2 * time.Minute}
}

// safeJarName keeps only the base name of a file name coming from a catalog, so a name like
// "../../x.jar" cannot write outside the mods directory. Names must end in ".jar".
func safeJarName(name string) (string, bool) {
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	if name == "." || name == "/" || strings.HasPrefix(name, ".") || !strings.HasSuffix(strings.ToLower(name), ".jar") {
		return "", false
	}
	return name, true
}

func (s *Server) downloadVerified(ctx context.Context, f modsource.File) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.URL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.modDownloader().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", modsource.ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: download returned %d", modsource.ErrUnavailable, resp.StatusCode)
	}
	limit := s.modMaxBytes()
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", modsource.ErrUnavailable, err)
	}
	if int64(len(b)) > limit {
		return nil, errModTooLarge
	}
	h, _, _ := modsource.Hashes(bytes.NewReader(b))
	if !strings.EqualFold(h.SHA1, f.SHA1) || (f.SHA512 != "" && !strings.EqualFold(h.SHA512, f.SHA512)) {
		return nil, errModHashMismatch
	}
	return b, nil
}

// installedFile is a jar in the mods directory with its hashes and metadata.
type installedFile struct {
	Name   string
	Size   int64
	Hashes modsource.FileHashes
	Meta   modsource.JarMeta
}

// installedHashes lists the jars of the mods directory, hashing only files the cache does not know
// (the key includes size and modification time).
func (s *Server) installedHashes(ctx context.Context, conn *sftpConn, t *modTargetInfo) ([]installedFile, error) {
	dir := path.Join("/", t.Mods.Directory)
	entries, err := conn.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []installedFile
	for _, e := range entries {
		if !e.Mode().IsRegular() || !strings.HasSuffix(strings.ToLower(e.Name()), ".jar") {
			continue
		}
		key := panelcache.ModHashKey{Namespace: t.GS.Namespace, GameServer: t.GS.Name, Path: path.Join(dir, e.Name()), Size: e.Size(), ModTime: e.ModTime().Unix()}
		if s.ModHashes != nil {
			if info, err := s.ModHashes.Get(ctx, key); err == nil && info != nil {
				out = append(out, installedFile{Name: e.Name(), Size: e.Size(), Hashes: info.Hashes, Meta: info.Meta})
				continue
			}
		}
		f, err := conn.Open(key.Path)
		if err != nil {
			return nil, err
		}
		content, err := io.ReadAll(io.LimitReader(f, s.modMaxBytes()))
		f.Close()
		if err != nil {
			return nil, err
		}
		info := jarInfo(content)
		if s.ModHashes != nil {
			_ = s.ModHashes.Set(ctx, key, info) // a cache outage only costs a re-read next time
		}
		out = append(out, installedFile{Name: e.Name(), Size: e.Size(), Hashes: info.Hashes, Meta: info.Meta})
	}
	return out, nil
}

func jarInfo(content []byte) modsource.JarInfo {
	h, _, _ := modsource.Hashes(bytes.NewReader(content))
	return modsource.JarInfo{Hashes: h, Meta: modsource.ReadJarMeta(content)}
}

// identified is what one source knows about an installed file.
type identified struct {
	source string
	match  modsource.Match
}

// identifyAll asks each source, in order, about the files the previous ones did not recognise. A
// source that fails becomes a warning; its files stay unknown.
func (s *Server) identifyAll(ctx context.Context, files []installedFile, f modsource.Filter) (map[string]identified, []string) {
	out := map[string]identified{}
	var warnings []string
	for _, name := range s.modSourceNames() {
		var pending []modsource.FileHashes
		for _, file := range files {
			if _, ok := out[file.Hashes.SHA1]; !ok {
				pending = append(pending, file.Hashes)
			}
		}
		if len(pending) == 0 {
			break
		}
		matches, err := s.ModSources[name].Identify(ctx, pending, f)
		if err != nil {
			warnings = append(warnings, name+" unavailable: "+err.Error())
			continue
		}
		for sha, m := range matches {
			out[sha] = identified{source: name, match: m}
		}
	}
	return out, warnings
}

// writeModFile writes content to dir/name through a temporary file and a rename, so the server
// never sees a half-written jar.
func writeModFile(conn *sftpConn, dir, name string, content []byte) error {
	if err := conn.MkdirAll(dir); err != nil {
		return err
	}
	final := path.Join(dir, name)
	tmp := path.Join(dir, "."+name+".part")
	f, err := conn.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		_ = conn.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = conn.Remove(tmp)
		return err
	}
	if _, err := conn.Stat(final); err == nil {
		if err := conn.Remove(final); err != nil {
			_ = conn.Remove(tmp)
			return err
		}
	}
	return conn.Rename(tmp, final)
}

type installModRequest struct {
	Source      string `json:"source"`
	ProjectID   string `json:"projectId"`
	VersionID   string `json:"versionId"`
	GameVersion string `json:"gameVersion"`
}

type installedEntry struct {
	ProjectID     string `json:"projectId"`
	VersionNumber string `json:"versionNumber"`
	File          string `json:"file"`
}

func (s *Server) handleInstallMod(w http.ResponseWriter, r *http.Request) {
	t, ok := s.modTarget(w, r)
	if !ok {
		return
	}
	var req installModRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	src, ok := s.modSource(w, req.Source)
	if !ok {
		return
	}
	conn, err := s.openFileSFTPClient(r.Context(), t.GS)
	if err != nil {
		writeFilesUnreachable(w, err)
		return
	}
	defer conn.Close()

	filter := modsource.Filter{Kind: modsource.Kind(t.Mods.Kind), Loaders: t.Mods.Loaders, GameVersion: req.GameVersion}
	if filter.GameVersion == "" && t.Mods.GameVersion.Variable != "" {
		if v := variableValue(t.GS, t.Egg, t.Mods.GameVersion.Variable); concreteVersion(v) {
			filter.GameVersion = strings.TrimSpace(v)
		}
	}
	if filter.GameVersion == "" && t.Mods.GameVersion.File != "" {
		filter.GameVersion = s.readAndRememberGameVersion(r.Context(), conn, t)
	}

	files, err := s.installedHashes(r.Context(), conn, t)
	if err != nil {
		writeError(w, statusForSFTP(err), err.Error())
		return
	}
	// Projects already on the server, by this source's project id → file name. Identification is
	// best effort: if the source cannot answer, nothing counts as installed.
	present := map[string]string{}
	if len(files) > 0 {
		hashes := make([]modsource.FileHashes, len(files))
		bySHA := map[string]string{}
		for i, f := range files {
			hashes[i] = f.Hashes
			bySHA[f.Hashes.SHA1] = f.Name
		}
		if matches, err := src.Identify(r.Context(), hashes, filter); err == nil {
			for sha, m := range matches {
				present[m.Project.ID] = bySHA[sha]
			}
		}
	}

	versions, skipped, err := modsource.ResolveInstall(r.Context(), modsource.ResolveInput{
		Source: src, ProjectID: req.ProjectID, VersionID: req.VersionID, Filter: filter,
		Installed: func(p string) bool { _, ok := present[p]; return ok },
	})
	switch {
	case errors.Is(err, modsource.ErrNoCompatibleVersion):
		writeError(w, http.StatusUnprocessableEntity, "no version compatible with this server was found")
		return
	case errors.Is(err, modsource.ErrTooManyDependencies):
		writeError(w, http.StatusUnprocessableEntity, "this mod needs too many dependencies to install automatically")
		return
	case err != nil:
		writeSourceError(w, err)
		return
	}

	existing := map[string]bool{}
	for _, f := range files {
		existing[f.Name] = true
	}
	var plan []plannedFile
	for _, v := range versions {
		if v.DistributionBlocked || v.File.URL == "" {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
				"error":   "the author does not allow this file to be downloaded by third parties: download it from its page",
				"pageUrl": v.PageURL,
			})
			return
		}
		name, ok := safeJarName(v.File.Filename)
		if !ok {
			writeError(w, http.StatusBadGateway, fmt.Sprintf("the source published an invalid file name %q", v.File.Filename))
			return
		}
		if existing[name] && present[v.ProjectID] != name {
			writeError(w, http.StatusConflict, fmt.Sprintf("a different file named %s already exists in %s", name, t.Mods.Directory))
			return
		}
		plan = append(plan, plannedFile{v: v, name: name})
	}
	// Download and verify everything before writing anything.
	for i := range plan {
		b, err := s.downloadVerified(r.Context(), plan[i].v.File)
		if err != nil {
			writeSourceError(w, err)
			return
		}
		plan[i].content = b
		plan[i].meta = modsource.ReadJarMeta(b)
	}
	plan, warnings := s.resolveJarDependencies(r.Context(), t, filter, files, existing, plan)
	dir := path.Join("/", t.Mods.Directory)
	resp := struct {
		Installed []installedEntry `json:"installed"`
		Skipped   []string         `json:"skipped"`
		Warnings  []string         `json:"warnings"`
	}{Installed: []installedEntry{}, Skipped: skipped, Warnings: warnings}
	if resp.Skipped == nil {
		resp.Skipped = []string{}
	}
	for _, p := range plan {
		if err := writeModFile(conn, dir, p.name, p.content); err != nil {
			writeError(w, statusForSFTP(err), err.Error())
			return
		}
		resp.Installed = append(resp.Installed, installedEntry{ProjectID: p.v.ProjectID, VersionNumber: p.v.VersionNumber, File: p.name})
	}
	s.auditMod(r, "mod.install", map[string]string{"source": req.Source, "project": req.ProjectID, "files": fmt.Sprint(len(plan))})
	writeJSON(w, http.StatusCreated, resp)
}

// plannedFile is one file an install will write.
type plannedFile struct {
	v       modsource.Version
	name    string
	content []byte
	meta    modsource.JarMeta
}

// jarDependencyRounds bounds how many times newly added jars are scanned for their own needs.
const jarDependencyRounds = 5

// resolveJarDependencies adds what the jars being installed require but nobody provides — the
// catalogs do not always list it (spark needs Fabric API only in its fabric.mod.json). Mods are
// looked up on Modrinth by mod id, with Fabric API modules ("fabric-*") falling back to the
// Fabric API project; plugins only get a warning, since a plugin name does not reliably map to
// a catalog project. Whatever cannot be resolved comes back as a warning, never an error.
func (s *Server) resolveJarDependencies(ctx context.Context, t *modTargetInfo, filter modsource.Filter, installed []installedFile, existing map[string]bool, plan []plannedFile) ([]plannedFile, []string) {
	warnings := []string{}
	provided := map[string]bool{}
	for _, f := range installed {
		for _, id := range f.Meta.Provides {
			provided[id] = true
		}
	}
	for _, p := range plan {
		for _, id := range p.meta.Provides {
			provided[id] = true
		}
	}
	inPlan := map[string]bool{}
	for _, p := range plan {
		inPlan[p.v.ProjectID] = true
	}
	modrinth := s.ModSources["modrinth"]
	attempted := map[string]bool{}
	for round := 0; round < jarDependencyRounds; round++ {
		type need struct{ id, by string }
		var needs []need
		for _, p := range plan {
			for _, id := range p.meta.Depends {
				if !provided[id] && !attempted[id] {
					attempted[id] = true
					needs = append(needs, need{id, p.name})
				}
			}
		}
		if len(needs) == 0 {
			break
		}
		for _, n := range needs {
			if provided[n.id] {
				continue // an earlier addition this round provides it
			}
			if t.Mods.Kind != gameserversv1alpha1.EggModsMod || modrinth == nil {
				warnings = append(warnings, fmt.Sprintf("%s requires %s, which is not installed: install it manually", n.by, n.id))
				continue
			}
			added, ok := s.fetchJarDependency(ctx, modrinth, n.id, filter, existing, inPlan)
			if !ok {
				warnings = append(warnings, fmt.Sprintf("%s requires %s, which could not be found automatically: install it manually", n.by, n.id))
				continue
			}
			if len(plan) >= maxInstallFilesPerRequest {
				warnings = append(warnings, fmt.Sprintf("%s requires %s, but this install already has too many files: install it manually", n.by, n.id))
				continue
			}
			plan = append(plan, *added)
			inPlan[added.v.ProjectID] = true
			for _, id := range added.meta.Provides {
				provided[id] = true
			}
		}
	}
	return plan, warnings
}

// maxInstallFilesPerRequest caps one install, dependencies included.
const maxInstallFilesPerRequest = 20

// fetchJarDependency finds, downloads and verifies the Modrinth project behind a mod id.
func (s *Server) fetchJarDependency(ctx context.Context, src modsource.Source, id string, filter modsource.Filter, existing, inPlan map[string]bool) (*plannedFile, bool) {
	candidates := []string{id}
	if strings.HasPrefix(id, "fabric-") && id != "fabric-api" {
		candidates = append(candidates, "fabric-api")
	}
	for _, c := range candidates {
		vs, err := src.Versions(ctx, c, filter)
		if err != nil || len(vs) == 0 {
			continue
		}
		v := vs[0]
		if inPlan[v.ProjectID] || v.DistributionBlocked || v.File.URL == "" {
			continue
		}
		name, ok := safeJarName(v.File.Filename)
		if !ok || existing[name] {
			continue
		}
		content, err := s.downloadVerified(ctx, v.File)
		if err != nil {
			continue
		}
		meta := modsource.ReadJarMeta(content)
		if !slices.Contains(meta.Provides, id) {
			continue // this project does not actually provide what was needed
		}
		return &plannedFile{v: v, name: name, content: content, meta: meta}, true
	}
	return nil, false
}

func (s *Server) auditMod(r *http.Request, action string, meta map[string]string) {
	acc := orgAccessFromContext(r.Context())
	if acc == nil {
		return
	}
	s.auditEvent(r, acc.Org.Slug, action, "gameserver", r.PathValue("name"), "success", meta)
}

// installedMod is one jar of the mods directory as the UI shows it.
type installedMod struct {
	File            string `json:"file"`
	Size            int64  `json:"size"`
	Source          string `json:"source,omitempty"`
	ProjectID       string `json:"projectId,omitempty"`
	Title           string `json:"title,omitempty"`
	IconURL         string `json:"iconUrl,omitempty"`
	PageURL         string `json:"pageUrl,omitempty"`
	Version         string `json:"version,omitempty"`
	LatestVersion   string `json:"latestVersion,omitempty"`
	LatestVersionID string `json:"latestVersionId,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable"`
}

func (s *Server) handleListInstalledMods(w http.ResponseWriter, r *http.Request) {
	t, ok := s.modTarget(w, r)
	if !ok {
		return
	}
	conn, err := s.openFileSFTPClient(r.Context(), t.GS)
	if err != nil {
		writeFilesUnreachable(w, err)
		return
	}
	defer conn.Close()
	files, err := s.installedHashes(r.Context(), conn, t)
	if err != nil {
		writeError(w, statusForSFTP(err), err.Error())
		return
	}
	gv := ""
	if t.Mods.GameVersion.Variable != "" {
		if v := variableValue(t.GS, t.Egg, t.Mods.GameVersion.Variable); concreteVersion(v) {
			gv = strings.TrimSpace(v)
		}
	}
	if gv == "" && t.Mods.GameVersion.File != "" {
		gv = s.readAndRememberGameVersion(r.Context(), conn, t)
	}
	known, warnings := s.identifyAll(r.Context(), files, modFilter(r, t, gv))

	items := make([]installedMod, 0, len(files))
	for _, f := range files {
		it := installedMod{File: f.Name, Size: f.Size}
		if id, ok := known[f.Hashes.SHA1]; ok {
			m := id.match
			it.Source, it.ProjectID, it.Title, it.IconURL, it.PageURL = id.source, m.Project.ID, m.Project.Title, m.Project.IconURL, m.Project.PageURL
			it.Version = m.Version.VersionNumber
			if m.Latest != nil {
				it.UpdateAvailable, it.LatestVersion, it.LatestVersionID = true, m.Latest.VersionNumber, m.Latest.ID
			}
		}
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i].Title, items[j].Title
		if a == "" {
			a = items[i].File
		}
		if b == "" {
			b = items[j].File
		}
		return strings.ToLower(a) < strings.ToLower(b)
	})
	if warnings == nil {
		warnings = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "warnings": warnings})
}

type modFileRequest struct {
	File string `json:"file"`
}

func (s *Server) handleUpdateMod(w http.ResponseWriter, r *http.Request) {
	t, ok := s.modTarget(w, r)
	if !ok {
		return
	}
	var req modFileRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if name, ok := safeJarName(req.File); !ok || name != req.File {
		writeError(w, http.StatusBadRequest, "file must be the name of a .jar in the mods directory")
		return
	}
	conn, err := s.openFileSFTPClient(r.Context(), t.GS)
	if err != nil {
		writeFilesUnreachable(w, err)
		return
	}
	defer conn.Close()
	files, err := s.installedHashes(r.Context(), conn, t)
	if err != nil {
		writeError(w, statusForSFTP(err), err.Error())
		return
	}
	var target *installedFile
	for i := range files {
		if files[i].Name == req.File {
			target = &files[i]
		}
	}
	if target == nil {
		writeError(w, http.StatusNotFound, "no such file in the mods directory")
		return
	}
	gv := s.readAndRememberGameVersion(r.Context(), conn, t)
	if t.Mods.GameVersion.Variable != "" {
		if v := variableValue(t.GS, t.Egg, t.Mods.GameVersion.Variable); concreteVersion(v) {
			gv = strings.TrimSpace(v)
		}
	}
	known, _ := s.identifyAll(r.Context(), []installedFile{*target}, modFilter(r, t, gv))
	id, ok := known[target.Hashes.SHA1]
	if !ok || id.match.Latest == nil {
		writeError(w, http.StatusConflict, "no update available for this file")
		return
	}
	latest := *id.match.Latest
	if latest.DistributionBlocked || latest.File.URL == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "the author does not allow this file to be downloaded by third parties", "pageUrl": latest.PageURL})
		return
	}
	name, ok := safeJarName(latest.File.Filename)
	if !ok {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("the source published an invalid file name %q", latest.File.Filename))
		return
	}
	content, err := s.downloadVerified(r.Context(), latest.File)
	if err != nil {
		writeSourceError(w, err)
		return
	}
	dir := path.Join("/", t.Mods.Directory)
	if err := writeModFile(conn, dir, name, content); err != nil {
		writeError(w, statusForSFTP(err), err.Error())
		return
	}
	if name != req.File {
		if err := conn.Remove(path.Join(dir, req.File)); err != nil {
			writeError(w, statusForSFTP(err), "new version written but the old file could not be removed: "+err.Error())
			return
		}
	}
	s.auditMod(r, "mod.update", map[string]string{"source": id.source, "project": latest.ProjectID, "from": req.File, "to": name})
	writeJSON(w, http.StatusOK, installedEntry{ProjectID: latest.ProjectID, VersionNumber: latest.VersionNumber, File: name})
}

func (s *Server) handleRemoveMod(w http.ResponseWriter, r *http.Request) {
	t, ok := s.modTarget(w, r)
	if !ok {
		return
	}
	file := r.URL.Query().Get("file")
	if name, ok := safeJarName(file); !ok || name != file {
		writeError(w, http.StatusBadRequest, "file must be the name of a .jar in the mods directory")
		return
	}
	conn, err := s.openFileSFTPClient(r.Context(), t.GS)
	if err != nil {
		writeFilesUnreachable(w, err)
		return
	}
	defer conn.Close()
	target := path.Join("/", t.Mods.Directory, file)
	if _, err := conn.Stat(target); err != nil {
		writeError(w, statusForSFTP(err), err.Error())
		return
	}
	if err := conn.Remove(target); err != nil {
		writeError(w, statusForSFTP(err), err.Error())
		return
	}
	s.auditMod(r, "mod.remove", map[string]string{"file": file})
	w.WriteHeader(http.StatusNoContent)
}
