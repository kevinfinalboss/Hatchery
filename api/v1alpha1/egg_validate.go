package v1alpha1

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
)

func (s *EggSpec) Validate() []string {
	var msgs []string
	if d := s.StartupDetection; d != nil {
		if _, err := regexp.Compile(d.Regex); err != nil {
			msgs = append(msgs, fmt.Sprintf("startupDetection.regex: %v", err))
		}
	}
	msgs = append(msgs, s.validateMods()...)
	return msgs
}

func (s *EggSpec) validateMods() []string {
	m := s.Mods
	if m == nil {
		return nil
	}
	var msgs []string
	if m.Kind != EggModsPlugin && m.Kind != EggModsMod {
		msgs = append(msgs, fmt.Sprintf("mods.kind: must be %q or %q", EggModsPlugin, EggModsMod))
	}
	if len(m.Loaders) == 0 {
		msgs = append(msgs, "mods.loaders: at least one loader is required")
	}
	for _, l := range m.Loaders {
		if !slices.Contains(KnownModLoaders, l) {
			msgs = append(msgs, fmt.Sprintf("mods.loaders: unknown loader %q", l))
		}
	}
	if !cleanRelative(m.Directory) {
		msgs = append(msgs, "mods.directory: must be a relative path inside the data directory")
	}
	if v := m.GameVersion.Variable; v != "" && !slices.ContainsFunc(s.Variables, func(e EggVariable) bool { return e.Name == v }) {
		msgs = append(msgs, fmt.Sprintf("mods.gameVersion.variable: %q is not declared in variables", v))
	}
	if f := m.GameVersion.File; f != "" && !cleanRelative(f) {
		msgs = append(msgs, "mods.gameVersion.file: must be a relative path inside the data directory")
	}
	return msgs
}

// cleanRelative: non-empty, relative, already clean and not escaping the base directory.
func cleanRelative(p string) bool {
	return p != "" && p != "." && !path.IsAbs(p) && path.Clean(p) == p && p != ".." && !strings.HasPrefix(p, "../")
}
