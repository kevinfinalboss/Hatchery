package v1alpha1

import (
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
)

func TestEggSpecValidate(t *testing.T) {
	ok := EggSpec{StartupDetection: &EggStartupDetection{Regex: `\)! For help, type `}}
	if msgs := ok.Validate(); len(msgs) != 0 {
		t.Fatalf("valid regex: got %v", msgs)
	}
	if msgs := (&EggSpec{}).Validate(); len(msgs) != 0 {
		t.Fatalf("no detection is valid: got %v", msgs)
	}
	bad := EggSpec{StartupDetection: &EggStartupDetection{Regex: `(unclosed`}}
	if msgs := bad.Validate(); len(msgs) != 1 {
		t.Fatalf("invalid regex must be reported once, got %v", msgs)
	}
}

func TestEggSpecValidateMods(t *testing.T) {
	valid := func() EggSpec {
		return EggSpec{
			Variables: []EggVariable{{Name: "MINECRAFT_VERSION"}},
			Mods: &EggMods{
				Kind:        EggModsPlugin,
				Loaders:     []string{"paper", "spigot"},
				Directory:   "plugins",
				GameVersion: EggModsGameVersion{Variable: "MINECRAFT_VERSION", File: ".hatchery/game-version"},
			},
		}
	}
	if s := valid(); len(s.Validate()) != 0 {
		t.Fatalf("valid mods block: got %v", s.Validate())
	}
	cases := map[string]func(*EggMods){
		"unknown kind":          func(m *EggMods) { m.Kind = "addon" },
		"no loaders":            func(m *EggMods) { m.Loaders = nil },
		"unknown loader":        func(m *EggMods) { m.Loaders = []string{"paper", "sponge"} },
		"empty directory":       func(m *EggMods) { m.Directory = "" },
		"dot directory":         func(m *EggMods) { m.Directory = "." },
		"absolute directory":    func(m *EggMods) { m.Directory = "/plugins" },
		"escaping directory":    func(m *EggMods) { m.Directory = "../plugins" },
		"unclean directory":     func(m *EggMods) { m.Directory = "a/../plugins" },
		"undeclared variable":   func(m *EggMods) { m.GameVersion.Variable = "MC_VERSION" },
		"absolute version file": func(m *EggMods) { m.GameVersion.File = "/data/version" },
	}
	for name, mutate := range cases {
		s := valid()
		mutate(s.Mods)
		if len(s.Validate()) == 0 {
			t.Errorf("%s: expected a validation message", name)
		}
	}
}

func TestEggSpecValidateBackup(t *testing.T) {
	ok := EggSpec{Backup: &EggBackup{Before: []string{"save-off", "save-all flush"}, After: []string{"save-on"}, SavedRegex: "Saved the game"}}
	if msgs := ok.Validate(); len(msgs) != 0 {
		t.Fatalf("valid backup block rejected: %v", msgs)
	}
	long := strings.Repeat("x", 513)
	cases := map[string]EggBackup{
		"empty command":     {Before: []string{""}},
		"multi-line":        {Before: []string{"save-off\nstop"}},
		"control character": {After: []string{"save-on\x07"}},
		"too long":          {After: []string{long}},
		"bad regex":         {SavedRegex: "("},
	}
	for name, b := range cases {
		spec := EggSpec{Backup: &b}
		if msgs := spec.Validate(); len(msgs) == 0 {
			t.Errorf("%s: expected a validation error", name)
		}
	}
	if d := (&EggBackup{}).Timeout(); d != 60*time.Second {
		t.Errorf("default timeout = %v", d)
	}
	if d := (&EggBackup{}).Delay(); d != 10*time.Second {
		t.Errorf("default delay = %v", d)
	}
	zero := int32(0)
	if d := (&EggBackup{DelaySeconds: &zero}).Delay(); d != 0 {
		t.Errorf("explicit zero delay = %v", d)
	}
}

func TestEggSpecValidateQuery(t *testing.T) {
	ports := []EggPort{
		{Name: "game", ContainerPort: 25565}, // empty protocol = TCP
		{Name: "steam", ContainerPort: 16262, Protocol: corev1.ProtocolUDP},
	}
	cases := []struct {
		name  string
		query *EggQuery
		ok    bool
	}{
		{"none", nil, true},
		{"minecraft on tcp (empty protocol)", &EggQuery{Protocol: EggQueryMinecraft, Port: "game"}, true},
		{"a2s on udp", &EggQuery{Protocol: EggQueryA2S, Port: "steam"}, true},
		{"unknown port", &EggQuery{Protocol: EggQueryMinecraft, Port: "rcon"}, false},
		{"minecraft on udp", &EggQuery{Protocol: EggQueryMinecraft, Port: "steam"}, false},
		{"a2s on tcp", &EggQuery{Protocol: EggQueryA2S, Port: "game"}, false},
		{"unknown protocol", &EggQuery{Protocol: "gamespy", Port: "game"}, false},
	}
	for _, c := range cases {
		s := EggSpec{Ports: ports, Query: c.query}
		msgs := s.Validate()
		if (len(msgs) == 0) != c.ok {
			t.Errorf("%s: Validate() = %v, want ok=%v", c.name, msgs, c.ok)
		}
		_, found := s.QueryPort()
		if found != (c.ok && c.query != nil) {
			t.Errorf("%s: QueryPort found = %v", c.name, found)
		}
	}
}
