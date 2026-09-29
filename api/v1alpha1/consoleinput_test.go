package v1alpha1

import (
	"slices"
	"testing"
)

func TestConsoleInputPath(t *testing.T) {
	if got := (&EggSpec{}).ConsoleInputPath(); got != DefaultConsoleInput {
		t.Fatalf("default: got %q", got)
	}
	s := EggSpec{ConsoleInput: "/tmp/minecraft-console-in"}
	if got := s.ConsoleInputPath(); got != "/tmp/minecraft-console-in" {
		t.Fatalf("declared: got %q", got)
	}
	if got := (&EggSpec{ConsoleInput: "/tmp/x'; reboot"}).ConsoleInputPath(); got != DefaultConsoleInput {
		t.Fatalf("invalid falls back: got %q", got)
	}
}

func TestConsoleLineCommandPassesPathAndLineAsArguments(t *testing.T) {
	got := ConsoleLineCommand("/tmp/pipe", `say "hi"; rm -rf /`)
	want := []string{"sh", "-c",
		`printf '%s\n' "$2" | dd of="$1" conv=nocreat,notrunc status=none 2>/dev/null || printf '%s\n' "$2" > "$1"`,
		"sh", "/tmp/pipe", `say "hi"; rm -rf /`}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestValidateConsoleInput(t *testing.T) {
	for _, p := range []string{"", "/proc/1/fd/0", "/tmp/minecraft-console-in"} {
		if msgs := (&EggSpec{ConsoleInput: p}).Validate(); len(msgs) != 0 {
			t.Errorf("%q: got %v", p, msgs)
		}
	}
	for _, p := range []string{"tmp/pipe", "/tmp/a b", "/tmp/a'b", "/tmp/a\nb", "/tmp/$(id)", "/" + string(make([]byte, 300))} {
		if msgs := (&EggSpec{ConsoleInput: p}).Validate(); len(msgs) != 1 {
			t.Errorf("%q: got %v", p, msgs)
		}
	}
}
