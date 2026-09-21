package panelapi

import (
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// maxSlugLen leaves room for suffixes ("-sftp", "-2") inside the 63-character label limit.
const maxSlugLen = 50

// slugify turns a display name into a DNS-1035-safe identifier: accents folded, [a-z0-9-] only,
// starting with a letter (the GameServer's Service name must).
func slugify(name string) string {
	var b strings.Builder
	lastDash := true
	for _, r := range norm.NFD.String(strings.ToLower(name)) {
		switch {
		case unicode.Is(unicode.Mn, r):
			continue
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "server"
	}
	if s[0] >= '0' && s[0] <= '9' {
		s = "server-" + s
	}
	if len(s) > maxSlugLen {
		s = strings.TrimRight(s[:maxSlugLen], "-")
	}
	return s
}

// uniqueSlug returns base, or base-2, base-3, … — the first one not in taken.
func uniqueSlug(base string, taken map[string]bool) string {
	if !taken[base] {
		return base
	}
	for i := 2; ; i++ {
		if cand := fmt.Sprintf("%s-%d", base, i); !taken[cand] {
			return cand
		}
	}
}
