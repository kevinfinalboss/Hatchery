package panelapi

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// ParseCIDRs parses a comma-separated CIDR list. An empty string means "none".
// An invalid entry is an error rather than being skipped: a typo silently
// dropping a proxy would make every client look like the proxy's IP.
func ParseCIDRs(csv string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, part := range strings.Split(csv, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		_, n, err := net.ParseCIDR(part)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %q: %w", part, err)
		}
		out = append(out, n)
	}
	return out, nil
}

func inNets(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func clientIP(r *http.Request, trusted []*net.IPNet) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer := net.ParseIP(host)
	if peer == nil || len(trusted) == 0 || !inNets(peer, trusted) {
		return host
	}

	entries := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(entries) - 1; i >= 0; i-- {
		ip := net.ParseIP(strings.TrimSpace(entries[i]))
		if ip == nil {
			return host // malformed header: do not guess
		}
		if !inNets(ip, trusted) {
			return ip.String()
		}
	}
	return host
}
