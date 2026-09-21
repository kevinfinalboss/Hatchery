package v1alpha1

import (
	"strings"
	"testing"
)

func eggWithVars() *Egg {
	return &Egg{Spec: EggSpec{Variables: []EggVariable{
		{Name: "MOTD", UserEditable: true, Default: "hi"},
		{Name: "PORT", UserEditable: true, Required: true, ValidationRegex: `^[0-9]+$`, Default: "7777"},
		{Name: "SECRET", UserEditable: false, Default: "x"},
	}}}
}

func TestValidateVariableOverrides(t *testing.T) {
	cases := []struct {
		name string
		vars []GameServerVariable
		want string // substring of the single expected message; "" = valid
	}{
		{"valid", []GameServerVariable{{Name: "MOTD", Value: "hello"}, {Name: "PORT", Value: "25565"}}, ""},
		{"unknown", []GameServerVariable{{Name: "NOPE", Value: "1"}}, `"NOPE" is not declared`},
		{"not editable", []GameServerVariable{{Name: "SECRET", Value: "y"}}, `"SECRET" is not editable`},
		{"regex", []GameServerVariable{{Name: "PORT", Value: "abc"}}, `"PORT" does not match`},
		{"required empty", []GameServerVariable{{Name: "PORT", Value: ""}}, `"PORT" is required`},
		{"duplicate", []GameServerVariable{{Name: "MOTD", Value: "a"}, {Name: "MOTD", Value: "b"}}, `"MOTD" is set more than once`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			msgs := ValidateVariableOverrides(eggWithVars(), c.vars)
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

func TestRequiredVariableWithDefaultIsFineWhenNotOverridden(t *testing.T) {
	if msgs := ValidateVariableOverrides(eggWithVars(), nil); len(msgs) != 0 {
		t.Fatalf("defaults satisfy required variables, got %v", msgs)
	}
}
