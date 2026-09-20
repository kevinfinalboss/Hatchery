package main

import (
	"fmt"
	"net"
	"strings"
)

// parseServiceAccount splits "<namespace>/<name>". Empty input means "not
// configured" and is not an error.
func parseServiceAccount(v string) (namespace, name string, err error) {
	if v == "" {
		return "", "", nil
	}
	parts := strings.Split(v, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("expected <namespace>/<name>, got %q", v)
	}
	return parts[0], parts[1], nil
}

// splitCSV splits a comma-separated list, trimming spaces and dropping empties.
func splitCSV(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseCIDRList splits a comma-separated CIDR list and rejects any entry that
// is not a valid CIDR. Checked at boot because the API server would otherwise
// reject the tenant NetworkPolicy at runtime, failing every tenant.
func parseCIDRList(v string) ([]string, error) {
	cidrs := splitCSV(v)
	for _, c := range cidrs {
		if _, _, err := net.ParseCIDR(c); err != nil {
			return nil, fmt.Errorf("invalid CIDR %q: %w", c, err)
		}
	}
	return cidrs, nil
}
