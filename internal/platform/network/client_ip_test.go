package network

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestClientIP(t *testing.T) {
	tests := []struct {
		name      string
		remote    string
		forwarded string
		trusted   []string
		want      string
	}{
		{name: "direct", remote: "203.0.113.10:443", want: "203.0.113.10"},
		{name: "trusted proxy", remote: "127.0.0.1:443", forwarded: "203.0.113.20", trusted: []string{"127.0.0.1/32"}, want: "203.0.113.20"},
		{name: "untrusted spoof", remote: "203.0.113.50:443", forwarded: "1.2.3.4", trusted: []string{"127.0.0.1/32"}, want: "203.0.113.50"},
		{name: "trusted chain", remote: "127.0.0.1:443", forwarded: "203.0.113.20, 172.16.0.10, 127.0.0.2", trusted: []string{"127.0.0.1/32", "127.0.0.0/8", "172.16.0.0/12"}, want: "203.0.113.20"},
		{name: "ipv6", remote: "[::1]:443", forwarded: "[2001:db8::20]:1234", trusted: []string{"::1/128"}, want: "2001:db8::20"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://example.test", nil)
			req.RemoteAddr = tt.remote
			if tt.forwarded != "" {
				req.Header.Set("X-Forwarded-For", tt.forwarded)
			}
			trusted := make([]netip.Prefix, 0, len(tt.trusted))
			for _, raw := range tt.trusted {
				trusted = append(trusted, netip.MustParsePrefix(raw))
			}
			got := ClientIP(req, trusted)
			if got.String() != tt.want {
				t.Fatalf("ClientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClientIPPrefersForwardedAndFallsBackSafely(t *testing.T) {
	req := httptest.NewRequest("GET", "http://example.test", nil)
	req.RemoteAddr = "127.0.0.1:443"
	req.Header.Set("Forwarded", `for=203.0.113.30;proto=https`)
	req.Header.Set("X-Forwarded-For", "198.51.100.30")
	got := ClientIP(req, []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")})
	if got.String() != "203.0.113.30" {
		t.Fatalf("ClientIP() = %q, want standardized Forwarded value", got)
	}
}

func TestClientIPUsesXRealIPAsLastFallback(t *testing.T) {
	req := httptest.NewRequest("GET", "http://example.test", nil)
	req.RemoteAddr = "127.0.0.1:443"
	req.Header.Set("X-Real-IP", "198.51.100.40")
	got := ClientIP(req, []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")})
	if got.String() != "198.51.100.40" {
		t.Fatalf("ClientIP() = %q, want X-Real-IP fallback", got)
	}
}
