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

package v1alpha1

import "strings"

// NormalizeImage turns an image reference into its full repository name, the way Docker resolves
// it: no registry means Docker Hub (docker.io/), and a one-segment name is an official image
// (docker.io/library/). Tag and digest are dropped and the result is lower-cased, so
// "itzg/minecraft-server:latest" and "docker.io/itzg/minecraft-server@sha256:…" compare equal.
func NormalizeImage(image string) string {
	name := strings.ToLower(strings.TrimSpace(image))
	if i := strings.IndexByte(name, '@'); i >= 0 {
		name = name[:i]
	}
	if i := strings.LastIndexByte(name, ':'); i > strings.LastIndexByte(name, '/') {
		name = name[:i] // a tag; a ':' before the last '/' is a registry port
	}
	return withRegistry(name)
}

// NormalizeRegistryEntry normalizes one allowlist entry so it compares against NormalizeImage's
// output. A single segment that does not look like a host is a Docker Hub user or org ("itzg").
func NormalizeRegistryEntry(entry string) string {
	e := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(entry)), "/")
	if e == "" {
		return ""
	}
	if !strings.Contains(e, "/") && !looksLikeHost(e) {
		return "docker.io/" + e
	}
	return withRegistry(e)
}

// withRegistry applies Docker's rule: the first segment is a registry only if it looks like a host.
func withRegistry(name string) string {
	first, rest, hasSlash := strings.Cut(name, "/")
	if first == "index.docker.io" {
		return "docker.io/" + rest
	}
	if looksLikeHost(first) {
		return name
	}
	if !hasSlash {
		return "docker.io/library/" + name
	}
	return "docker.io/" + name
}

func looksLikeHost(s string) bool {
	return strings.ContainsAny(s, ".:") || s == "localhost"
}

// ImageAllowed reports whether image comes from one of the allowed registries. Matching is by
// whole path segments: "docker.io/itzg" allows docker.io/itzg/*, never docker.io/itzgevil/*. An
// empty allowlist allows everything (the check is off).
func ImageAllowed(image string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	img := NormalizeImage(image)
	for _, a := range allowed {
		p := NormalizeRegistryEntry(a)
		if p == "" {
			continue
		}
		if img == p || strings.HasPrefix(img, p+"/") {
			return true
		}
	}
	return false
}

// ImageRefs lists every image the Egg can make the cluster run: its selectable images plus the
// install and configure images when set.
func (s EggSpec) ImageRefs() []string {
	var out []string
	for _, i := range s.Images {
		if i.Image != "" {
			out = append(out, i.Image)
		}
	}
	if s.Install != nil && s.Install.Image != "" {
		out = append(out, s.Install.Image)
	}
	if s.Configure != nil && s.Configure.Image != "" {
		out = append(out, s.Configure.Image)
	}
	return out
}
