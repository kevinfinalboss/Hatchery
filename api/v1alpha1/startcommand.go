package v1alpha1

import (
	"fmt"
	"regexp"
	"unicode"
)

// MaxStartCommandLength bounds GameServer.spec.startCommand (mirrored by the CRD's MaxLength).
const MaxStartCommandLength = 4096

var startPlaceholder = regexp.MustCompile(`\{\{([A-Za-z_][A-Za-z0-9_]*)\}\}`)

// EffectiveStartCommand returns the GameServer's own start command, or the Egg's when it has none.
func (e *Egg) EffectiveStartCommand(override string) string {
	if override != "" {
		return override
	}
	return e.Spec.StartCommand
}

// ValidateStartCommand checks a GameServer's start command override against its Egg and returns one
// message per problem (nil when valid; empty is valid and means "use the Egg's").
func ValidateStartCommand(egg *Egg, cmd string) []string {
	if cmd == "" {
		return nil
	}
	if len(cmd) > MaxStartCommandLength {
		return []string{fmt.Sprintf("start command must have at most %d characters", MaxStartCommandLength)}
	}
	for _, r := range cmd {
		if r == '\n' || r == '\r' {
			return []string{"start command must be a single line (use bash -c '…' for several steps)"}
		}
		if unicode.IsControl(r) {
			return []string{"start command must not contain control characters"}
		}
	}
	declared := make(map[string]bool, len(egg.Spec.Variables))
	for _, v := range egg.Spec.Variables {
		declared[v.Name] = true
	}
	var msgs []string
	seen := map[string]bool{}
	for _, m := range startPlaceholder.FindAllStringSubmatch(cmd, -1) {
		if !declared[m[1]] && !seen[m[1]] {
			seen[m[1]] = true
			msgs = append(msgs, fmt.Sprintf("placeholder {{%s}} is not a variable declared by egg %q", m[1], egg.Name))
		}
	}
	return msgs
}
