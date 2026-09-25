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

// Package modsource talks to the mod/plugin catalogs (Modrinth, CurseForge) behind one interface,
// so the Panel can search, pick versions with their required dependencies, and recognise files
// already on a server by hash.
package modsource

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Kind is what a server takes: plugins (Paper & co.) or mods (Fabric & co.).
type Kind string

const (
	KindPlugin Kind = "plugin"
	KindMod    Kind = "mod"
	// KindModpack is a whole pack (loader, Minecraft version, mods, configs) that builds a server.
	KindModpack Kind = "modpack"
)

// Filter narrows results to what a server can run. GameVersion "" means any version.
type Filter struct {
	Kind        Kind
	Loaders     []string
	GameVersion string
}

// SearchQuery is one page (20 results, 0-based) of a text search.
type SearchQuery struct {
	Filter
	Query string
	Page  int
}

// PageSize is how many results a search page holds.
const PageSize = 20

type SearchResult struct {
	Source              string `json:"source"`
	ProjectID           string `json:"projectId"`
	Slug                string `json:"slug"`
	Title               string `json:"title"`
	Description         string `json:"description"`
	IconURL             string `json:"iconUrl"`
	Author              string `json:"author"`
	PageURL             string `json:"pageUrl"`
	Downloads           int64  `json:"downloads"`
	DistributionBlocked bool   `json:"distributionBlocked"`
}

type SearchPage struct {
	Results []SearchResult `json:"results"`
	Total   int            `json:"total"`
}

// File is a version's primary file. SHA512 may be empty (CurseForge only publishes sha1/md5).
type File struct {
	URL      string `json:"-"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	SHA1     string `json:"-"`
	SHA512   string `json:"-"`
}

// Dependency is a required dependency; VersionID "" means any compatible version.
type Dependency struct {
	ProjectID string `json:"projectId"`
	VersionID string `json:"versionId,omitempty"`
}

type Version struct {
	Source              string       `json:"source"`
	ID                  string       `json:"id"`
	ProjectID           string       `json:"projectId"`
	Name                string       `json:"name"`
	VersionNumber       string       `json:"versionNumber"`
	GameVersions        []string     `json:"gameVersions"`
	Loaders             []string     `json:"loaders"`
	File                File         `json:"file"`
	Dependencies        []Dependency `json:"dependencies"`
	PublishedAt         time.Time    `json:"publishedAt"`
	DistributionBlocked bool         `json:"distributionBlocked"`
	PageURL             string       `json:"pageUrl"`
}

type Project struct {
	Source  string `json:"source"`
	ID      string `json:"id"`
	Slug    string `json:"slug"`
	Title   string `json:"title"`
	IconURL string `json:"iconUrl"`
	PageURL string `json:"pageUrl"`
}

// Match is what a source knows about a file it recognised. Latest is nil when Version already is
// the newest version compatible with the filter.
type Match struct {
	Project Project
	Version Version
	Latest  *Version
}

// Source is one mod catalog.
type Source interface {
	Name() string
	Search(ctx context.Context, q SearchQuery) (SearchPage, error)
	// Versions lists the project's versions compatible with f, newest first.
	Versions(ctx context.Context, projectID string, f Filter) ([]Version, error)
	// Identify recognises files by hash; the map is keyed by FileHashes.SHA1 and holds only the
	// files the source knows.
	Identify(ctx context.Context, files []FileHashes, f Filter) (map[string]Match, error)
}

var (
	// ErrUnavailable covers network errors and 5xx answers: the source is down or unreachable.
	ErrUnavailable = errors.New("mod source unavailable")
	// ErrNotFound is a 404 from the source.
	ErrNotFound = errors.New("not found in mod source")
)

// RateLimitedError is a 429 from the source.
type RateLimitedError struct{ RetryAfter time.Duration }

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("mod source rate limit reached, retry in %s", e.RetryAfter)
}
