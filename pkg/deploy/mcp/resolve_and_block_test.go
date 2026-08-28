package mcp

import (
	"strings"
	"testing"
)

// resolveAndBlock is the SSRF gate used by validateHelmRepoURL and the OCI
// chart ref path (tools_helm.go:171). go tool cover reports 63.6% on this
// function; the previously-uncovered arms are:
//
//   - host is a literal IP that isHelmBlockedIP accepts (loopback/private/
//     link-local/CGNAT/cloud-metadata/IETF-reserved) -- must return
//     "resolves to blocked IP" without calling the DNS resolver.
//   - host is a hostname whose DNS lookup errors -- must return
//     "DNS lookup failed" wrapping the resolver error.
//   - host is a hostname whose resolver returns at least one blocked IP
//     alongside a benign one -- must still refuse.
//   - host is a hostname whose resolver returns only non-parseable strings
//     -- ParseIP returns nil for each, the block loop skips them, and the
//     function returns nil (defense-in-depth against garbage resolver
//     output).
//
// A regression that turned any of these arms into a silent pass or a
// panic would open an SSRF hole into private networks / IMDS.
func TestResolveAndBlock_LiteralBlockedIPReturnsError(t *testing.T) {
	for _, ip := range []string{
		"127.0.0.1",       // loopback
		"10.0.0.1",        // RFC1918 private
		"169.254.169.254", // cloud metadata (link-local)
		"100.64.0.1",      // CGNAT
	} {
		t.Run(ip, func(t *testing.T) {
			// Fail loudly if the resolver is called at all for a literal IP.
			setHelmMockResolver(t, func(host string) ([]string, error) {
				t.Fatalf("resolver must not be called for literal IP, got %q", host)
				return nil, nil
			})
			err := resolveAndBlock(ip)
			if err == nil {
				t.Fatalf("expected error for blocked literal IP %q, got nil", ip)
			}
			if !strings.Contains(err.Error(), "resolves to blocked IP") {
				t.Fatalf("expected 'resolves to blocked IP' in %q", err.Error())
			}
		})
	}
}

func TestResolveAndBlock_LiteralPublicIPIsAllowed(t *testing.T) {
	setHelmMockResolver(t, func(host string) ([]string, error) {
		t.Fatalf("resolver must not be called for literal IP, got %q", host)
		return nil, nil
	})
	if err := resolveAndBlock("93.184.216.34"); err != nil {
		t.Fatalf("expected nil for public literal IP, got %v", err)
	}
}

func TestResolveAndBlock_DNSLookupFailurePropagates(t *testing.T) {
	setHelmMockResolver(t, func(host string) ([]string, error) {
		return nil, errStubDNS{}
	})
	err := resolveAndBlock("no-such-host.invalid")
	if err == nil {
		t.Fatal("expected DNS lookup failure error, got nil")
	}
	if !strings.Contains(err.Error(), "DNS lookup failed") {
		t.Fatalf("expected 'DNS lookup failed' in %q", err.Error())
	}
}

func TestResolveAndBlock_HostnameResolvesToBlockedIP(t *testing.T) {
	// Resolver returns a benign IP first, then a blocked one -- the loop
	// must catch the blocked entry even if it comes second.
	setHelmMockResolver(t, func(host string) ([]string, error) {
		return []string{"93.184.216.34", "169.254.169.254"}, nil
	})
	err := resolveAndBlock("evil.example.com")
	if err == nil {
		t.Fatal("expected blocked-IP error, got nil")
	}
	if !strings.Contains(err.Error(), "resolves to blocked IP") {
		t.Fatalf("expected 'resolves to blocked IP' in %q", err.Error())
	}
}

func TestResolveAndBlock_HostnameResolvesToOnlyPublicIPs(t *testing.T) {
	setHelmMockResolver(t, func(host string) ([]string, error) {
		return []string{"93.184.216.34", "8.8.8.8"}, nil
	})
	if err := resolveAndBlock("cdn.example.com"); err != nil {
		t.Fatalf("expected nil for all-public resolution, got %v", err)
	}
}

func TestResolveAndBlock_UnparseableResolverOutputIsSkipped(t *testing.T) {
	// If the resolver hands back garbage strings, net.ParseIP returns nil
	// and the loop must skip rather than crash. Regression guard against
	// a future refactor that dropped the "ip != nil" guard on line 183.
	setHelmMockResolver(t, func(host string) ([]string, error) {
		return []string{"not-an-ip", "also-garbage"}, nil
	})
	if err := resolveAndBlock("weird.example.com"); err != nil {
		t.Fatalf("expected nil when resolver output is unparseable, got %v", err)
	}
}

type errStubDNS struct{}

func (errStubDNS) Error() string { return "stub: DNS lookup failed" }
