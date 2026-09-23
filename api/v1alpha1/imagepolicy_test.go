package v1alpha1

import "testing"

func TestNormalizeImage(t *testing.T) {
	cases := map[string]string{
		"itzg/minecraft-server:latest":         "docker.io/itzg/minecraft-server",
		"alpine":                               "docker.io/library/alpine",
		"alpine:3.20":                          "docker.io/library/alpine",
		"docker.io/itzg/minecraft-server":      "docker.io/itzg/minecraft-server",
		"index.docker.io/itzg/mc":              "docker.io/itzg/mc",
		"ghcr.io/ptero-eggs/yolks:java_21":     "ghcr.io/ptero-eggs/yolks",
		"ghcr.io/ptero-eggs/yolks@sha256:abcd": "ghcr.io/ptero-eggs/yolks",
		"registry.lan:5000/team/img:1":         "registry.lan:5000/team/img",
		"localhost/img":                        "localhost/img",
		"GHCR.io/Ptero-Eggs/Yolks":             "ghcr.io/ptero-eggs/yolks",
	}
	for in, want := range cases {
		if got := NormalizeImage(in); got != want {
			t.Errorf("NormalizeImage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestImageAllowed(t *testing.T) {
	allowed := []string{"docker.io/itzg", "ghcr.io", "GHCR.io/Ptero-Eggs/", "registry.lan:5000/team"}
	yes := []string{
		"itzg/minecraft-server:latest",
		"docker.io/itzg/minecraft-server",
		"ghcr.io/anyone/anything:1",
		"ghcr.io/ptero-eggs/yolks@sha256:abcd",
		"registry.lan:5000/team/img:2",
	}
	no := []string{
		"itzgevil/minecraft-server", // prefix by string, not by segment
		"alpine",                    // docker.io/library/alpine
		"quay.io/itzg/minecraft-server",
		"registry.lan:5000/teamx/img",
		"registry.lan/team/img", // different registry host (no port)
	}
	for _, img := range yes {
		if !ImageAllowed(img, allowed) {
			t.Errorf("ImageAllowed(%q) = false, want true", img)
		}
	}
	for _, img := range no {
		if ImageAllowed(img, allowed) {
			t.Errorf("ImageAllowed(%q) = true, want false", img)
		}
	}
	if !ImageAllowed("anything/at:all", nil) {
		t.Error("an empty allowlist must allow everything (check disabled)")
	}
	if !ImageAllowed("alpine", []string{"docker.io/library"}) {
		t.Error("docker.io/library must allow official images")
	}
	if !ImageAllowed("itzg/minecraft-server", []string{"itzg"}) {
		t.Error(`the entry "itzg" must mean the Docker Hub user docker.io/itzg`)
	}
}

func TestEggSpecImageRefs(t *testing.T) {
	s := EggSpec{
		Images:    []EggImage{{Name: "a", Image: "img/a:1"}, {Name: "b", Image: "img/b:1"}},
		Install:   &EggInstall{Image: ""},
		Configure: &EggConfigure{Image: "img/c:1"},
	}
	got := s.ImageRefs()
	want := []string{"img/a:1", "img/b:1", "img/c:1"}
	if len(got) != len(want) {
		t.Fatalf("ImageRefs() = %v, want %v (empty install.image must be skipped)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ImageRefs() = %v, want %v", got, want)
		}
	}
}
