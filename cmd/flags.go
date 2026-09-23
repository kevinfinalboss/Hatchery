package main

import (
	"fmt"
	"net"
	"os"
	"strconv"
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

// envOr returns the environment variable's value, or fallback when it is unset or empty.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// parsePublicPortRange parses "<min>-<max>" (e.g. "30000-40000"). Empty input means the public
// exposure feature is disabled and is not an error: both returned values are 0, the sentinel
// cmd/main.go uses to skip registering the GatewayExposureReconciler entirely.
func parsePublicPortRange(v string) (min, max int32, err error) {
	if v == "" {
		return 0, 0, nil
	}
	parts := strings.SplitN(v, "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected <min>-<max>, got %q", v)
	}
	lo, err := strconv.ParseInt(parts[0], 10, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid min port %q: %w", parts[0], err)
	}
	hi, err := strconv.ParseInt(parts[1], 10, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid max port %q: %w", parts[1], err)
	}
	if lo <= 0 || hi <= 0 || lo > hi || hi > 65535 {
		return 0, 0, fmt.Errorf("invalid port range %q: must be 1-65535 and min <= max", v)
	}
	return int32(lo), int32(hi), nil
}
