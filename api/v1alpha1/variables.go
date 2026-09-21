package v1alpha1

import (
	"fmt"
	"regexp"
)

func ValidateVariableOverrides(egg *Egg, vars []GameServerVariable) []string {
	declared := make(map[string]EggVariable, len(egg.Spec.Variables))
	resolved := make(map[string]string, len(egg.Spec.Variables))
	for _, v := range egg.Spec.Variables {
		declared[v.Name] = v
		resolved[v.Name] = v.Default
	}

	var msgs []string
	seen := make(map[string]bool, len(vars))
	for _, o := range vars {
		d, ok := declared[o.Name]
		switch {
		case !ok:
			msgs = append(msgs, fmt.Sprintf("variable %q is not declared by egg %q", o.Name, egg.Name))
			continue
		case seen[o.Name]:
			msgs = append(msgs, fmt.Sprintf("variable %q is set more than once", o.Name))
			continue
		case !d.UserEditable:
			msgs = append(msgs, fmt.Sprintf("variable %q is not editable", o.Name))
			continue
		}
		seen[o.Name] = true
		resolved[o.Name] = o.Value
		if d.ValidationRegex == "" || o.Value == "" {
			continue
		}
		re, err := regexp.Compile(d.ValidationRegex)
		if err != nil {
			msgs = append(msgs, fmt.Sprintf("variable %q has an invalid validationRegex in egg %q", o.Name, egg.Name))
		} else if !re.MatchString(o.Value) {
			msgs = append(msgs, fmt.Sprintf("variable %q does not match %s", o.Name, d.ValidationRegex))
		}
	}
	for _, d := range egg.Spec.Variables {
		if d.Required && resolved[d.Name] == "" {
			msgs = append(msgs, fmt.Sprintf("variable %q is required and cannot be empty", d.Name))
		}
	}
	return msgs
}
