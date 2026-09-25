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
	"errors"
)

const (
	maxDependencyDepth = 5
	maxInstallFiles    = 20
)

var (
	ErrNoCompatibleVersion = errors.New("no compatible version")
	ErrTooManyDependencies = errors.New("too many dependencies")
)

// ResolveInput describes one install request. Installed reports whether a project (any version)
// is already on the server.
type ResolveInput struct {
	Source    Source
	ProjectID string
	VersionID string // "" = newest compatible
	Filter    Filter
	Installed func(projectID string) bool
}

// ResolveInstall returns the versions to install: the requested one first, then its required
// dependencies breadth-first, skipping projects already installed or already chosen (which also
// breaks dependency cycles). The requested project itself is never skipped.
func ResolveInstall(ctx context.Context, in ResolveInput) (install []Version, skipped []string, err error) {
	type item struct {
		projectID, versionID string
		depth                int
	}
	queue := []item{{in.ProjectID, in.VersionID, 0}}
	seen := map[string]bool{in.ProjectID: true}
	for len(queue) > 0 {
		it := queue[0]
		queue = queue[1:]
		if it.depth > 0 && in.Installed(it.projectID) {
			skipped = append(skipped, it.projectID)
			continue
		}
		v, err := pickVersion(ctx, in.Source, it.projectID, it.versionID, in.Filter)
		if err != nil {
			return nil, nil, err
		}
		install = append(install, v)
		if len(install) > maxInstallFiles {
			return nil, nil, ErrTooManyDependencies
		}
		if it.depth >= maxDependencyDepth {
			continue
		}
		for _, d := range v.Dependencies {
			if !seen[d.ProjectID] {
				seen[d.ProjectID] = true
				queue = append(queue, item{d.ProjectID, d.VersionID, it.depth + 1})
			}
		}
	}
	return install, skipped, nil
}

func pickVersion(ctx context.Context, src Source, projectID, versionID string, f Filter) (Version, error) {
	vs, err := src.Versions(ctx, projectID, f)
	if err != nil {
		return Version{}, err
	}
	for _, v := range vs {
		if versionID == "" || v.ID == versionID {
			return v, nil
		}
	}
	return Version{}, ErrNoCompatibleVersion
}
