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
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"path"

	"sigs.k8s.io/yaml"
)

// JarMeta is what a mod/plugin jar says about itself: the ids it provides (its own, its
// aliases and, for Fabric, the mods it embeds) and the ids it requires. Catalogs do not always
// list every requirement (spark needs Fabric API but only its fabric.mod.json says so).
type JarMeta struct {
	Provides []string `json:"provides,omitempty"`
	Depends  []string `json:"depends,omitempty"`
}

// JarInfo is everything the Panel keeps about an installed jar: its hashes (to recognise it in
// the catalogs) and its metadata (to know what it provides and requires).
type JarInfo struct {
	Hashes FileHashes `json:"hashes"`
	Meta   JarMeta    `json:"meta"`
}

// platformIDs are requirements the server itself satisfies.
var platformIDs = map[string]bool{"minecraft": true, "java": true, "fabricloader": true, "fabric": true, "quilt_loader": true}

// maxNestedDepth bounds how deep embedded jars (jar-in-jar) are followed.
const maxNestedDepth = 3

// ReadJarMeta reads fabric.mod.json, plugin.yml or paper-plugin.yml from a jar. Anything
// unreadable yields an empty JarMeta: metadata only improves dependency handling, it never
// blocks an install.
func ReadJarMeta(content []byte) JarMeta {
	return readJarMeta(content, 0)
}

func readJarMeta(content []byte, depth int) JarMeta {
	zr, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return JarMeta{}
	}
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	if f, ok := files["fabric.mod.json"]; ok {
		return fabricMeta(files, f, depth)
	}
	for _, name := range []string{"plugin.yml", "paper-plugin.yml"} {
		if f, ok := files[name]; ok {
			return pluginMeta(f)
		}
	}
	return JarMeta{}
}

func readZipFile(f *zip.File, limit int64) []byte {
	rc, err := f.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()
	b, _ := io.ReadAll(io.LimitReader(rc, limit))
	return b
}

func fabricMeta(files map[string]*zip.File, f *zip.File, depth int) JarMeta {
	var mod struct {
		ID       string                     `json:"id"`
		Provides []string                   `json:"provides"`
		Depends  map[string]json.RawMessage `json:"depends"`
		Jars     []struct {
			File string `json:"file"`
		} `json:"jars"`
	}
	if err := json.Unmarshal(readZipFile(f, 1<<20), &mod); err != nil || mod.ID == "" {
		return JarMeta{}
	}
	meta := JarMeta{Provides: append([]string{mod.ID}, mod.Provides...)}
	for id := range mod.Depends {
		if !platformIDs[id] {
			meta.Depends = append(meta.Depends, id)
		}
	}
	if depth < maxNestedDepth {
		for _, j := range mod.Jars {
			nf, ok := files[path.Clean(j.File)]
			if !ok {
				continue
			}
			// Embedded mods satisfy requirements; their own requirements are the embedding
			// mod's business, so only what they provide is kept.
			meta.Provides = append(meta.Provides, readJarMeta(readZipFile(nf, 64<<20), depth+1).Provides...)
		}
	}
	return meta
}

func pluginMeta(f *zip.File) JarMeta {
	var p struct {
		Name     string   `json:"name"`
		Provides []string `json:"provides"`
		Depend   []string `json:"depend"`
	}
	if err := yaml.Unmarshal(readZipFile(f, 1<<20), &p); err != nil || p.Name == "" {
		return JarMeta{}
	}
	return JarMeta{Provides: append([]string{p.Name}, p.Provides...), Depends: p.Depend}
}
