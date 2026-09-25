package v1alpha1

import "testing"

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
