package netguard

import (
	"net"
	"testing"
)

// TestIsBlockedIP exercises every range IsBlockedIP documents as blocked,
// plus the public-IP allow path. This package is built standalone (no
// -coverpkg in CI's `go test ./...`), so coverage exercised only through
// callers in pkg/deploy/mcp/helm and pkg/gitops does not register here --
// see kubestellar-mcp#964/#1 (extraction precedent) for why a dedicated
// test file is required for every predicate this package owns.
func TestIsBlockedIP(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want bool
	}{
		{"loopback v4", "127.0.0.1", true},
		{"loopback v6", "::1", true},
		{"private 10/8", "10.0.0.1", true},
		{"private 172.16/12", "172.16.0.1", true},
		{"private 192.168/16", "192.168.1.1", true},
		{"private v6 ULA", "fd00::1", true},
		{"link-local unicast v4", "169.254.1.1", true},
		{"link-local unicast v6", "fe80::1", true},
		{"unspecified v4", "0.0.0.0", true},
		{"unspecified v6", "::", true},
		{"CGNAT 100.64/10 start", "100.64.0.0", true},
		{"CGNAT 100.64/10 mid", "100.64.0.1", true},
		{"CGNAT 100.64/10 end", "100.127.255.255", true},
		{"CGNAT boundary just below", "100.63.255.255", false},
		{"CGNAT boundary just above", "100.128.0.0", false},
		{"cloud metadata 169.254.169.254", "169.254.169.254", true},
		{"IETF reserved 192.0.0.0/24", "192.0.0.1", true},
		{"IETF reserved boundary below", "192.0.1.0", false},
		{"public v4", "8.8.8.8", false},
		{"public v4 other", "1.1.1.1", false},
		{"public v6", "2001:4860:4860::8888", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("net.ParseIP(%q) returned nil", tt.ip)
			}
			got := IsBlockedIP(ip)
			if got != tt.want {
				t.Errorf("IsBlockedIP(%s) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

// TestErrBlockedIPSentinel guards the errors.Is contract that
// pkg/metrics.ClassifyError relies on to classify SSRF-guard rejections
// under a bounded error_kind label instead of falling into "unknown".
func TestErrBlockedIPSentinel(t *testing.T) {
	if ErrBlockedIP == nil {
		t.Fatal("ErrBlockedIP must not be nil")
	}
	if ErrBlockedIP.Error() == "" {
		t.Fatal("ErrBlockedIP must have a non-empty message")
	}
}
