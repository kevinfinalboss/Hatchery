package modsource

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// fakeSource serves one version per project; deps maps project -> required projects.
type fakeSource struct{ deps map[string][]string }

func (f *fakeSource) Name() string { return "fake" }
func (f *fakeSource) Search(context.Context, SearchQuery) (SearchPage, error) {
	return SearchPage{}, nil
}
func (f *fakeSource) Identify(context.Context, []FileHashes, Filter) (map[string]Match, error) {
	return nil, nil
}
func (f *fakeSource) Versions(_ context.Context, projectID string, _ Filter) ([]Version, error) {
	if projectID == "missing" {
		return nil, nil
	}
	v := Version{Source: "fake", ID: projectID + "-v1", ProjectID: projectID, File: File{Filename: projectID + ".jar"}}
	for _, d := range f.deps[projectID] {
		v.Dependencies = append(v.Dependencies, Dependency{ProjectID: d})
	}
	return []Version{v}, nil
}

func ids(vs []Version) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.ProjectID
	}
	return out
}

func TestResolveInstallTransitiveAndSkipped(t *testing.T) {
	src := &fakeSource{deps: map[string][]string{"a": {"b", "lib"}, "b": {"c"}}}
	got, skipped, err := ResolveInstall(context.Background(), ResolveInput{
		Source: src, ProjectID: "a", Installed: func(p string) bool { return p == "lib" },
	})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(ids(got)) != "[a b c]" || fmt.Sprint(skipped) != "[lib]" {
		t.Fatalf("install %v skipped %v", ids(got), skipped)
	}
}

func TestResolveInstallCycle(t *testing.T) {
	src := &fakeSource{deps: map[string][]string{"a": {"b"}, "b": {"a"}}}
	got, _, err := ResolveInstall(context.Background(), ResolveInput{Source: src, ProjectID: "a", Installed: func(string) bool { return false }})
	if err != nil || fmt.Sprint(ids(got)) != "[a b]" {
		t.Fatalf("got %v, %v", ids(got), err)
	}
}

func TestResolveInstallRequestedVersionMustExist(t *testing.T) {
	src := &fakeSource{}
	if _, _, err := ResolveInstall(context.Background(), ResolveInput{Source: src, ProjectID: "a", VersionID: "nope", Installed: func(string) bool { return false }}); !errors.Is(err, ErrNoCompatibleVersion) {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := ResolveInstall(context.Background(), ResolveInput{Source: src, ProjectID: "missing", Installed: func(string) bool { return false }}); !errors.Is(err, ErrNoCompatibleVersion) {
		t.Fatalf("no versions: err = %v", err)
	}
}

func TestResolveInstallTooMany(t *testing.T) {
	deps := map[string][]string{}
	for i := 0; i < 21; i++ {
		deps["a"] = append(deps["a"], fmt.Sprintf("d%d", i))
	}
	if _, _, err := ResolveInstall(context.Background(), ResolveInput{Source: &fakeSource{deps: deps}, ProjectID: "a", Installed: func(string) bool { return false }}); !errors.Is(err, ErrTooManyDependencies) {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveInstallRequestedEvenIfInstalled(t *testing.T) {
	// Reinstalling/updating the requested project itself is not skipped.
	got, _, err := ResolveInstall(context.Background(), ResolveInput{Source: &fakeSource{}, ProjectID: "a", Installed: func(string) bool { return true }})
	if err != nil || len(got) != 1 {
		t.Fatalf("got %v, %v", ids(got), err)
	}
}
