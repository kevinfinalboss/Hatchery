package panelapi

import (
	"strings"
	"testing"
)

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Survival dos Amigos": "survival-dos-amigos",
		"Servidor Ação!! 2":   "servidor-acao-2",
		"123 jogo":            "server-123-jogo",
		"🔥🔥":                  "server",
		"  --A--  ":           "a",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
	if got := slugify(strings.Repeat("a", 90)); len(got) != 50 {
		t.Errorf("must cap at 50 characters, got %d", len(got))
	}
}

func TestUniqueSlug(t *testing.T) {
	if got := uniqueSlug("abc", map[string]bool{"abc": true, "abc-2": true}); got != "abc-3" {
		t.Fatalf("got %q", got)
	}
	if got := uniqueSlug("abc", nil); got != "abc" {
		t.Fatalf("got %q", got)
	}
}
