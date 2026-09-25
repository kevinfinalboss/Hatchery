package v1alpha1

import (
	"os"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestSampleEggsWithModsValidate(t *testing.T) {
	want := map[string]EggMods{
		"gameservers_v1alpha1_egg_paper.yaml":  {Kind: EggModsPlugin, Directory: "plugins"},
		"gameservers_v1alpha1_egg_purpur.yaml": {Kind: EggModsPlugin, Directory: "plugins"},
		"gameservers_v1alpha1_egg_fabric.yaml": {Kind: EggModsMod, Directory: "mods"},
	}
	for file, w := range want {
		raw, err := os.ReadFile("../../config/samples/" + file)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		var egg Egg
		if err := yaml.UnmarshalStrict(raw, &egg); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if msgs := egg.Spec.Validate(); len(msgs) != 0 {
			t.Errorf("%s: %v", file, msgs)
		}
		m := egg.Spec.Mods
		if m == nil || m.Kind != w.Kind || m.Directory != w.Directory || m.GameVersion.File != ".hatchery/game-version" {
			t.Errorf("%s: mods = %+v", file, m)
		}
		if egg.Spec.Configure == nil {
			t.Errorf("%s: missing configure step (server.properties port/ip)", file)
		}
	}
}
