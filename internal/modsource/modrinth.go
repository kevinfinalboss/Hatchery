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

package modsource

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Modrinth is the Modrinth catalog (https://docs.modrinth.com/api/). Reads need no key, but a
// User-Agent identifying the application is required; the limit is 300 requests/min per IP.
type Modrinth struct {
	http httpClient

	// compat caches "project has a version for this loader set and game version" verdicts, so a
	// repeated search does not re-check the same projects.
	compatMu sync.Mutex
	compat   map[string]compatVerdict
}

type compatVerdict struct {
	ok      bool
	expires time.Time
}

// compatTTL is how long a compatibility verdict is reused.
const compatTTL = time.Hour

func NewModrinth(userAgent string) *Modrinth {
	return &Modrinth{http: newHTTPClient("https://api.modrinth.com/v2", userAgent, nil), compat: map[string]compatVerdict{}}
}

func (m *Modrinth) Name() string { return "modrinth" }

type mrHit struct {
	ProjectID     string `json:"project_id"`
	Slug          string `json:"slug"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	IconURL       string `json:"icon_url"`
	Author        string `json:"author"`
	Downloads     int64  `json:"downloads"`
	ProjectType   string `json:"project_type"`
	LatestVersion string `json:"latest_version"`
}

type mrVersion struct {
	ID            string   `json:"id"`
	ProjectID     string   `json:"project_id"`
	Name          string   `json:"name"`
	VersionNumber string   `json:"version_number"`
	GameVersions  []string `json:"game_versions"`
	Loaders       []string `json:"loaders"`
	DatePublished string   `json:"date_published"`
	Files         []struct {
		URL      string            `json:"url"`
		Filename string            `json:"filename"`
		Size     int64             `json:"size"`
		Primary  bool              `json:"primary"`
		Hashes   map[string]string `json:"hashes"`
	} `json:"files"`
	Dependencies []struct {
		ProjectID      string `json:"project_id"`
		VersionID      string `json:"version_id"`
		DependencyType string `json:"dependency_type"`
	} `json:"dependencies"`
}

type mrProject struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	IconURL     string `json:"icon_url"`
	ProjectType string `json:"project_type"`
}

func mrPageURL(projectType, slug string) string {
	if projectType == "" {
		projectType = "project"
	}
	return "https://modrinth.com/" + projectType + "/" + slug
}

// mrProjectType maps our Kind to Modrinth's project_type (Paper plugins are "plugin" there).
func mrProjectType(k Kind) string {
	switch k {
	case KindPlugin:
		return "plugin"
	case KindModpack:
		return "modpack"
	}
	return "mod"
}

func jsonList(vs []string) string {
	b, _ := json.Marshal(vs)
	return string(b)
}

func (m *Modrinth) Search(ctx context.Context, q SearchQuery) (SearchPage, error) {
	facets := [][]string{{"project_type:" + mrProjectType(q.Kind)}}
	if len(q.Loaders) > 0 {
		var ls []string
		for _, l := range q.Loaders {
			ls = append(ls, "categories:"+l)
		}
		facets = append(facets, ls)
	}
	if q.GameVersion != "" {
		facets = append(facets, []string{"versions:" + q.GameVersion})
	}
	fb, _ := json.Marshal(facets)
	params := url.Values{
		"query":  {q.Query},
		"limit":  {strconv.Itoa(PageSize)},
		"offset": {strconv.Itoa(q.Page * PageSize)},
		"index":  {"relevance"},
		"facets": {string(fb)},
	}
	var resp struct {
		Hits      []mrHit `json:"hits"`
		TotalHits int     `json:"total_hits"`
	}
	if err := m.http.getJSON(ctx, "/search", params, &resp); err != nil {
		return SearchPage{}, err
	}
	hits := resp.Hits
	if q.GameVersion != "" && len(q.Loaders) > 0 {
		// Modrinth's facets match per project: a mod can have a Fabric build and a 26.3 build
		// without one build being both. Keep only projects with a version matching both.
		var err error
		if hits, err = m.keepCompatible(ctx, hits, q.Filter); err != nil {
			return SearchPage{}, err
		}
	}
	page := SearchPage{Total: resp.TotalHits, Results: make([]SearchResult, 0, len(hits))}
	for _, h := range hits {
		page.Results = append(page.Results, SearchResult{
			Source: m.Name(), ProjectID: h.ProjectID, Slug: h.Slug, Title: h.Title, Description: h.Description,
			IconURL: h.IconURL, Author: h.Author, Downloads: h.Downloads, PageURL: mrPageURL(h.ProjectType, h.Slug),
		})
	}
	return page, nil
}

func matchesFilter(loaders, gameVersions []string, f Filter) bool {
	if !slices.Contains(gameVersions, f.GameVersion) {
		return false
	}
	return slices.ContainsFunc(loaders, func(l string) bool { return slices.Contains(f.Loaders, l) })
}

func compatKey(projectID string, f Filter) string {
	ls := slices.Clone(f.Loaders)
	slices.Sort(ls)
	return projectID + "|" + strings.Join(ls, ",") + "|" + f.GameVersion
}

// keepCompatible drops hits without a version matching the filter's loaders and game version.
// The newest version of every hit is fetched in one batch; only hits whose newest version does
// not match get a per-project lookup. Verdicts are cached for compatTTL.
func (m *Modrinth) keepCompatible(ctx context.Context, hits []mrHit, f Filter) ([]mrHit, error) {
	now := time.Now()
	verdict := map[string]bool{}
	var latestIDs []string
	m.compatMu.Lock()
	for _, h := range hits {
		if v, ok := m.compat[compatKey(h.ProjectID, f)]; ok && now.Before(v.expires) {
			verdict[h.ProjectID] = v.ok
		} else if h.LatestVersion != "" {
			latestIDs = append(latestIDs, h.LatestVersion)
		}
	}
	m.compatMu.Unlock()

	if len(latestIDs) > 0 {
		var latest []mrVersion
		if err := m.http.getJSON(ctx, "/versions", url.Values{"ids": {jsonList(latestIDs)}}, &latest); err != nil {
			return nil, err
		}
		for _, v := range latest {
			if matchesFilter(v.Loaders, v.GameVersions, f) {
				verdict[v.ProjectID] = true
			}
		}
	}
	for _, h := range hits {
		if _, known := verdict[h.ProjectID]; known {
			continue
		}
		vs, err := m.Versions(ctx, h.ProjectID, f)
		if err != nil {
			return nil, err
		}
		verdict[h.ProjectID] = len(vs) > 0
	}

	m.compatMu.Lock()
	for id, ok := range verdict {
		m.compat[compatKey(id, f)] = compatVerdict{ok: ok, expires: now.Add(compatTTL)}
	}
	m.compatMu.Unlock()

	out := hits[:0:0]
	for _, h := range hits {
		if verdict[h.ProjectID] {
			out = append(out, h)
		}
	}
	return out, nil
}

func (m *Modrinth) Versions(ctx context.Context, projectID string, f Filter) ([]Version, error) {
	params := url.Values{}
	if len(f.Loaders) > 0 {
		params.Set("loaders", jsonList(f.Loaders))
	}
	if f.GameVersion != "" {
		params.Set("game_versions", jsonList([]string{f.GameVersion}))
	}
	var raw []mrVersion
	if err := m.http.getJSON(ctx, "/project/"+url.PathEscape(projectID)+"/version", params, &raw); err != nil {
		return nil, err
	}
	out := make([]Version, 0, len(raw))
	for _, v := range raw {
		out = append(out, m.version(v))
	}
	return out, nil // Modrinth already answers newest first
}

func (m *Modrinth) version(v mrVersion) Version {
	out := Version{
		Source: m.Name(), ID: v.ID, ProjectID: v.ProjectID, Name: v.Name, VersionNumber: v.VersionNumber,
		GameVersions: v.GameVersions, Loaders: v.Loaders,
		PageURL: "https://modrinth.com/project/" + v.ProjectID + "/version/" + v.ID,
	}
	out.PublishedAt, _ = time.Parse(time.RFC3339, v.DatePublished)
	for i, f := range v.Files {
		if f.Primary || i == 0 {
			out.File = File{URL: f.URL, Filename: f.Filename, Size: f.Size, SHA1: f.Hashes["sha1"], SHA512: f.Hashes["sha512"]}
			if f.Primary {
				break
			}
		}
	}
	for _, d := range v.Dependencies {
		if d.DependencyType == "required" && d.ProjectID != "" {
			out.Dependencies = append(out.Dependencies, Dependency{ProjectID: d.ProjectID, VersionID: d.VersionID})
		}
	}
	return out
}

func (m *Modrinth) Identify(ctx context.Context, files []FileHashes, f Filter) (map[string]Match, error) {
	out := map[string]Match{}
	if len(files) == 0 {
		return out, nil
	}
	hashes := make([]string, 0, len(files))
	for _, fh := range files {
		hashes = append(hashes, fh.SHA1)
	}
	var current map[string]mrVersion
	if err := m.http.postJSON(ctx, "/version_files", map[string]any{"hashes": hashes, "algorithm": "sha1"}, &current); err != nil {
		return nil, err
	}
	if len(current) == 0 {
		return out, nil
	}
	body := map[string]any{"hashes": hashes, "algorithm": "sha1", "loaders": f.Loaders}
	if f.GameVersion != "" {
		body["game_versions"] = []string{f.GameVersion}
	}
	var latest map[string]mrVersion
	if err := m.http.postJSON(ctx, "/version_files/update", body, &latest); err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for _, v := range current {
		ids[v.ProjectID] = true
	}
	projects, err := m.projects(ctx, ids)
	if err != nil {
		return nil, err
	}
	for hash, cv := range current {
		match := Match{Project: projects[cv.ProjectID], Version: m.version(cv)}
		if lv, ok := latest[hash]; ok {
			l := m.version(lv)
			if IsNewer(l, match.Version) {
				match.Latest = &l
			}
		}
		out[hash] = match
	}
	return out, nil
}

func (m *Modrinth) projects(ctx context.Context, ids map[string]bool) (map[string]Project, error) {
	list := make([]string, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	var raw []mrProject
	if err := m.http.getJSON(ctx, "/projects", url.Values{"ids": {jsonList(list)}}, &raw); err != nil {
		return nil, err
	}
	out := map[string]Project{}
	for _, p := range raw {
		out[p.ID] = Project{Source: m.Name(), ID: p.ID, Slug: p.Slug, Title: p.Title, IconURL: p.IconURL, PageURL: mrPageURL(p.ProjectType, p.Slug)}
	}
	return out, nil
}

// IsNewer reports whether candidate is a real update over current: another version, published
// later. The catalogs' "newest compatible" can be older than what is installed (a file built for a
// newer game version than the server's), which is not an update.
func IsNewer(candidate, current Version) bool {
	return candidate.ID != current.ID && candidate.PublishedAt.After(current.PublishedAt)
}
