package v1alpha1

import (
	"os"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestSampleEggsWithModsValidate(t *testing.T) {
	want := map[string]EggMods{
		"gameservers_v1alpha1_egg_paper.yaml":    {Kind: EggModsPlugin, Directory: "plugins"},
		"gameservers_v1alpha1_egg_purpur.yaml":   {Kind: EggModsPlugin, Directory: "plugins"},
		"gameservers_v1alpha1_egg_fabric.yaml":   {Kind: EggModsMod, Directory: "mods"},
		"gameservers_v1alpha1_egg_neoforge.yaml": {Kind: EggModsMod, Directory: "mods"},
		"gameservers_v1alpha1_egg_quilt.yaml":    {Kind: EggModsMod, Directory: "mods"},
		"gameservers_v1alpha1_egg_spigot.yaml":   {Kind: EggModsPlugin, Directory: "plugins"},
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

func TestYolksEggsRunAsSFTPUser(t *testing.T) {
	for _, file := range []string{"paper", "purpur", "fabric", "neoforge", "quilt", "spigot", "terraria", "zomboid"} {
		raw, err := os.ReadFile("../../config/samples/gameservers_v1alpha1_egg_" + file + ".yaml")
		if err != nil {
			t.Fatal(err)
		}
		var egg Egg
		if err := yaml.UnmarshalStrict(raw, &egg); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if egg.Spec.RunAsUser == nil || *egg.Spec.RunAsUser != 1000 {
			t.Errorf("%s: runAsUser = %v, want 1000", file, egg.Spec.RunAsUser)
		}
	}
}

func TestModpackEggSample(t *testing.T) {
	raw, err := os.ReadFile("../../config/samples/gameservers_v1alpha1_egg_modpack.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var egg Egg
	if err := yaml.UnmarshalStrict(raw, &egg); err != nil {
		t.Fatal(err)
	}
	if !egg.IsModpack() || egg.Spec.Mods != nil || egg.Spec.RunAsUser != nil {
		t.Fatalf("modpack egg: modpack=%v mods=%v runAsUser=%v", egg.IsModpack(), egg.Spec.Mods, egg.Spec.RunAsUser)
	}
	if msgs := egg.Spec.Validate(); len(msgs) != 0 {
		t.Fatal(msgs)
	}
	var names []string
	for _, img := range egg.Spec.Images {
		names = append(names, img.Name)
	}
	if strings.Join(names, ",") != "Java 25,Java 21,Java 17,Java 8" {
		t.Fatalf("images = %v (the Panel picks them by these names)", names)
	}
	if errs := ValidateStartCommand(&egg, egg.Spec.StartCommand); len(errs) != 0 {
		t.Fatalf("start command: %v", errs)
	}
}
