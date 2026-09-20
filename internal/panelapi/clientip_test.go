package panelapi

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func reqFrom(remote, xff string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = remote
	if xff != "" {
		r.Header.Set("X-Forwarded-For", xff)
	}
	return r
}

func TestClientIP(t *testing.T) {
	trusted, err := ParseCIDRs("10.0.0.0/8, 192.168.1.0/24")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		r       *http.Request
		trusted bool
		want    string
	}{
		{"no proxies configured: the header is ignored, it is trivially forgeable", reqFrom("203.0.113.9:5555", "1.2.3.4"), false, "203.0.113.9"},
		{"untrusted peer sending XFF is ignored", reqFrom("203.0.113.9:5555", "1.2.3.4"), true, "203.0.113.9"},
		{"trusted proxy: the real client is the entry it appended", reqFrom("10.1.1.1:80", "198.51.100.7"), true, "198.51.100.7"},
		{"a client-forged first entry is skipped: walk from the right past trusted hops", reqFrom("10.1.1.1:80", "6.6.6.6, 198.51.100.7, 10.2.2.2"), true, "198.51.100.7"},
		{"trusted proxy but no header: fall back to the peer", reqFrom("10.1.1.1:80", ""), true, "10.1.1.1"},
		{"garbage in XFF falls back to the peer", reqFrom("10.1.1.1:80", "not-an-ip"), true, "10.1.1.1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var set []*net.IPNet
			if c.trusted {
				set = trusted
			}
			if got := clientIP(c.r, set); got != c.want {
				t.Fatalf("clientIP = %q, want %q", got, c.want)
			}
		})
	}
}

func TestParseCIDRs(t *testing.T) {
	if got, err := ParseCIDRs(""); err != nil || got != nil {
		t.Fatalf("empty must mean none, got %v, %v", got, err)
	}
	if _, err := ParseCIDRs("10.0.0.0/8, nonsense"); err == nil {
		t.Fatal("an invalid CIDR must be an error, not silently dropped")
	}
}
