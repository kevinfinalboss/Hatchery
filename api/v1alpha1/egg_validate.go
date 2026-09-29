package v1alpha1

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"

	corev1 "k8s.io/api/core/v1"
)

func (s *EggSpec) Validate() []string {
	var msgs []string
	if d := s.StartupDetection; d != nil {
		if _, err := regexp.Compile(d.Regex); err != nil {
			msgs = append(msgs, fmt.Sprintf("startupDetection.regex: %v", err))
		}
	}
	if p := s.ConsoleInput; p != "" && !consoleInputPattern.MatchString(p) {
		msgs = append(msgs, "consoleInput: must be an absolute path of letters, digits, '.', '_', '-' and '/' (at most 256)")
	}
	msgs = append(msgs, s.validateMods()...)
	msgs = append(msgs, s.validateBackup()...)
	msgs = append(msgs, s.validateQuery()...)
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

func (s *EggSpec) validateBackup() []string {
	b := s.Backup
	if b == nil {
		return nil
	}
	var msgs []string
	check := func(field string, cmds []string) {
		for i, c := range cmds {
			if c == "" || len(c) > 512 || strings.IndexFunc(c, unicode.IsControl) >= 0 {
				msgs = append(msgs, fmt.Sprintf("backup.%s[%d] must be one line of 1 to 512 characters", field, i))
			}
		}
	}
	check("before", b.Before)
	check("after", b.After)
	if b.SavedRegex != "" {
		if _, err := regexp.Compile(b.SavedRegex); err != nil {
			msgs = append(msgs, fmt.Sprintf("backup.savedRegex: %v", err))
		}
	}
	return msgs
}

// queryProtocolTransport is the port protocol each query protocol talks over.
var queryProtocolTransport = map[EggQueryProtocol]corev1.Protocol{
	EggQueryMinecraft: corev1.ProtocolTCP,
	EggQueryA2S:       corev1.ProtocolUDP,
}

// queryPort finds the port spec.query names and the transport the protocol needs.
func (s *EggSpec) queryPort() (port EggPort, found bool, want corev1.Protocol, known bool) {
	want, known = queryProtocolTransport[s.Query.Protocol]
	for _, p := range s.Ports {
		if p.Name == s.Query.Port {
			if p.Protocol == "" {
				p.Protocol = corev1.ProtocolTCP
			}
			return p, true, want, known
		}
	}
	return EggPort{}, false, want, known
}

// QueryPort returns the port spec.query points at, when the query is usable: the named port
// exists and speaks the transport the protocol needs.
func (s *EggSpec) QueryPort() (EggPort, bool) {
	if s.Query == nil {
		return EggPort{}, false
	}
	p, found, want, known := s.queryPort()
	return p, found && known && p.Protocol == want
}

func (s *EggSpec) validateQuery() []string {
	q := s.Query
	if q == nil {
		return nil
	}
	p, found, want, known := s.queryPort()
	switch {
	case !known:
		return []string{fmt.Sprintf("query.protocol: must be %q or %q", EggQueryMinecraft, EggQueryA2S)}
	case !found:
		return []string{fmt.Sprintf("query.port: %q is not declared in ports", q.Port)}
	case p.Protocol != want:
		return []string{fmt.Sprintf("query.port: %q is %s, but %s needs %s", q.Port, p.Protocol, q.Protocol, want)}
	}
	return nil
}
