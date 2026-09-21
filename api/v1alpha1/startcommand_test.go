package v1alpha1

import (
	"strings"
	"testing"
)

func TestValidateStartCommand(t *testing.T) {
	egg := &Egg{Spec: EggSpec{Variables: []EggVariable{{Name: "PORT"}, {Name: "MAX_PLAYERS"}}}}
	cases := []struct {
		name string
		cmd  string
		want string
	}{
		{"empty means the Egg's own command", "", ""},
		{"plain command", "java -jar server.jar", ""},
		{"declared placeholders", "run --port {{PORT}} --max {{MAX_PLAYERS}}", ""},
		{"undeclared placeholder", "run {{NOPE}}", "{{NOPE}}"},
		{"newline", "run\nrm -rf /data", "single line"},
		{"control character", "run\x07", "control characters"},
		{"too long", strings.Repeat("a", MaxStartCommandLength+1), "at most"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			msgs := ValidateStartCommand(egg, c.cmd)
			if c.want == "" {
				if len(msgs) != 0 {
					t.Fatalf("expected valid, got %v", msgs)
				}
				return
			}
			if len(msgs) != 1 || !strings.Contains(msgs[0], c.want) {
				t.Fatalf("expected one message containing %q, got %v", c.want, msgs)
			}
		})
	}
}

func TestEffectiveStartCommand(t *testing.T) {
	egg := &Egg{Spec: EggSpec{StartCommand: "egg default"}}
	if got := egg.EffectiveStartCommand(""); got != "egg default" {
		t.Fatalf("empty override must fall back to the Egg's command, got %q", got)
	}
	if got := egg.EffectiveStartCommand("mine"); got != "mine" {
		t.Fatalf("got %q", got)
	}
}
