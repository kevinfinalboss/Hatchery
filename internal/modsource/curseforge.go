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
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// CurseForge IDs and enums, confirmed against the live API on 2026-09-25: game and class IDs via
// /v1/games and /v1/categories; modLoaderType 4 returns Fabric files; relationType 3 is a required
// dependency; hash algo 1 is SHA1.
const (
	cfGameMinecraft      = 432
	cfClassMods          = 6
	cfClassBukkitPlugins = 5
	cfRelationRequired   = 3
	cfHashSHA1           = 1
)

var cfLoaderType = map[string]int{"forge": 1, "fabric": 4, "quilt": 5, "neoforge": 6}

// CurseForge is the CurseForge catalog (https://docs.curseforge.com/rest-api/). It needs an API
// key; authors may disable third-party distribution, in which case files carry no download URL.
type CurseForge struct {
	http httpClient
}

func NewCurseForge(apiKey, userAgent string) *CurseForge {
	return &CurseForge{http: newHTTPClient("https://api.curseforge.com", userAgent, http.Header{"X-Api-Key": {apiKey}})}
}

func (c *CurseForge) Name() string { return "curseforge" }

type cfMod struct {
	ID                   int    `json:"id"`
	Name                 string `json:"name"`
	Slug                 string `json:"slug"`
	Summary              string `json:"summary"`
	DownloadCount        int64  `json:"downloadCount"`
	AllowModDistribution *bool  `json:"allowModDistribution"`
	Logo                 *struct {
		ThumbnailURL string `json:"thumbnailUrl"`
	} `json:"logo"`
	Links struct {
		WebsiteURL string `json:"websiteUrl"`
	} `json:"links"`
	Authors []struct {
		Name string `json:"name"`
	} `json:"authors"`
}

type cfFile struct {
	ID           int      `json:"id"`
	ModID        int      `json:"modId"`
	DisplayName  string   `json:"displayName"`
	FileName     string   `json:"fileName"`
	FileLength   int64    `json:"fileLength"`
	DownloadURL  string   `json:"downloadUrl"`
	FileDate     string   `json:"fileDate"`
	GameVersions []string `json:"gameVersions"`
	Fingerprint  uint32   `json:"fileFingerprint"`
	Hashes       []struct {
		Value string `json:"value"`
		Algo  int    `json:"algo"`
	} `json:"hashes"`
	Dependencies []struct {
		ModID        int `json:"modId"`
		RelationType int `json:"relationType"`
	} `json:"dependencies"`
}

// cfClassAndLoader: plugins live in the Bukkit Plugins class and have no loader type; mods use the
// first of the Egg's loaders CurseForge knows.
func cfClassAndLoader(f Filter) (classID, loader int) {
	if f.Kind == KindPlugin {
		return cfClassBukkitPlugins, 0
	}
	for _, l := range f.Loaders {
		if t, ok := cfLoaderType[l]; ok {
			return cfClassMods, t
		}
	}
	return cfClassMods, 0
}

func (c *CurseForge) Search(ctx context.Context, q SearchQuery) (SearchPage, error) {
	classID, loader := cfClassAndLoader(q.Filter)
	params := url.Values{
		"gameId":       {strconv.Itoa(cfGameMinecraft)},
		"classId":      {strconv.Itoa(classID)},
		"searchFilter": {q.Query},
		"sortField":    {"2"}, // popularity
		"sortOrder":    {"desc"},
		"index":        {strconv.Itoa(q.Page * PageSize)},
		"pageSize":     {strconv.Itoa(PageSize)},
	}
	if loader != 0 {
		params.Set("modLoaderType", strconv.Itoa(loader))
	}
	if q.GameVersion != "" {
		params.Set("gameVersion", q.GameVersion)
	}
	var resp struct {
		Data       []cfMod `json:"data"`
		Pagination struct {
			TotalCount int `json:"totalCount"`
		} `json:"pagination"`
	}
	if err := c.http.getJSON(ctx, "/v1/mods/search", params, &resp); err != nil {
		return SearchPage{}, err
	}
	page := SearchPage{Total: resp.Pagination.TotalCount, Results: make([]SearchResult, 0, len(resp.Data))}
	for _, m := range resp.Data {
		r := SearchResult{
			Source: c.Name(), ProjectID: strconv.Itoa(m.ID), Slug: m.Slug, Title: m.Name, Description: m.Summary,
			Downloads: m.DownloadCount, PageURL: m.Links.WebsiteURL,
			DistributionBlocked: m.AllowModDistribution != nil && !*m.AllowModDistribution,
		}
		if m.Logo != nil {
			r.IconURL = m.Logo.ThumbnailURL
		}
		if len(m.Authors) > 0 {
			r.Author = m.Authors[0].Name
		}
		page.Results = append(page.Results, r)
	}
	return page, nil
}

func (c *CurseForge) Versions(ctx context.Context, projectID string, f Filter) ([]Version, error) {
	_, loader := cfClassAndLoader(f)
	params := url.Values{"pageSize": {"50"}}
	if loader != 0 {
		params.Set("modLoaderType", strconv.Itoa(loader))
	}
	if f.GameVersion != "" {
		params.Set("gameVersion", f.GameVersion)
	}
	var resp struct {
		Data []cfFile `json:"data"`
	}
	if err := c.http.getJSON(ctx, "/v1/mods/"+url.PathEscape(projectID)+"/files", params, &resp); err != nil {
		return nil, err
	}
	out := make([]Version, 0, len(resp.Data))
	for _, file := range resp.Data {
		out = append(out, c.version(file))
	}
	sortNewestFirst(out)
	return out, nil
}

func sortNewestFirst(vs []Version) {
	sort.SliceStable(vs, func(i, j int) bool { return vs[i].PublishedAt.After(vs[j].PublishedAt) })
}

func (c *CurseForge) version(f cfFile) Version {
	v := Version{
		Source: c.Name(), ID: strconv.Itoa(f.ID), ProjectID: strconv.Itoa(f.ModID), Name: f.DisplayName,
		VersionNumber: f.DisplayName, File: File{URL: f.DownloadURL, Filename: f.FileName, Size: f.FileLength},
		DistributionBlocked: f.DownloadURL == "",
	}
	v.PublishedAt, _ = time.Parse(time.RFC3339, f.FileDate)
	for _, gv := range f.GameVersions {
		if _, isLoader := cfLoaderType[strings.ToLower(gv)]; isLoader {
			v.Loaders = append(v.Loaders, strings.ToLower(gv))
		} else {
			v.GameVersions = append(v.GameVersions, gv)
		}
	}
	for _, h := range f.Hashes {
		if h.Algo == cfHashSHA1 {
			v.File.SHA1 = h.Value
		}
	}
	for _, d := range f.Dependencies {
		if d.RelationType == cfRelationRequired {
			v.Dependencies = append(v.Dependencies, Dependency{ProjectID: strconv.Itoa(d.ModID)})
		}
	}
	return v
}

// compatible reports whether a file fits the filter: the game version (when known) and, for mods,
// one of the loaders.
func compatible(v Version, f Filter) bool {
	if f.GameVersion != "" && !slices.Contains(v.GameVersions, f.GameVersion) {
		return false
	}
	if f.Kind == KindMod && len(v.Loaders) > 0 {
		return slices.ContainsFunc(v.Loaders, func(l string) bool { return slices.Contains(f.Loaders, l) })
	}
	return true
}

func (c *CurseForge) Identify(ctx context.Context, files []FileHashes, f Filter) (map[string]Match, error) {
	out := map[string]Match{}
	if len(files) == 0 {
		return out, nil
	}
	bySum := map[uint32]string{}
	fps := make([]uint32, 0, len(files))
	for _, fh := range files {
		bySum[fh.Fingerprint] = fh.SHA1
		fps = append(fps, fh.Fingerprint)
	}
	var resp struct {
		Data struct {
			ExactMatches []struct {
				ID          int      `json:"id"`
				File        cfFile   `json:"file"`
				LatestFiles []cfFile `json:"latestFiles"`
			} `json:"exactMatches"`
		} `json:"data"`
	}
	if err := c.http.postJSON(ctx, "/v1/fingerprints/"+strconv.Itoa(cfGameMinecraft), map[string]any{"fingerprints": fps}, &resp); err != nil {
		return nil, err
	}
	if len(resp.Data.ExactMatches) == 0 {
		return out, nil
	}
	ids := make([]int, 0, len(resp.Data.ExactMatches))
	for _, m := range resp.Data.ExactMatches {
		ids = append(ids, m.ID)
	}
	projects, err := c.projects(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, m := range resp.Data.ExactMatches {
		sha, ok := bySum[m.File.Fingerprint]
		if !ok {
			continue
		}
		match := Match{Project: projects[m.ID], Version: c.version(m.File)}
		var best *Version
		for _, lf := range m.LatestFiles {
			v := c.version(lf)
			if compatible(v, f) && (best == nil || v.PublishedAt.After(best.PublishedAt)) {
				best = &v
			}
		}
		if best != nil && IsNewer(*best, match.Version) {
			match.Latest = best
		}
		out[sha] = match
	}
	return out, nil
}

func (c *CurseForge) projects(ctx context.Context, ids []int) (map[int]Project, error) {
	var resp struct {
		Data []cfMod `json:"data"`
	}
	if err := c.http.postJSON(ctx, "/v1/mods", map[string]any{"modIds": ids}, &resp); err != nil {
		return nil, err
	}
	out := map[int]Project{}
	for _, m := range resp.Data {
		p := Project{Source: c.Name(), ID: strconv.Itoa(m.ID), Slug: m.Slug, Title: m.Name, PageURL: m.Links.WebsiteURL}
		if m.Logo != nil {
			p.IconURL = m.Logo.ThumbnailURL
		}
		out[m.ID] = p
	}
	return out, nil
}
