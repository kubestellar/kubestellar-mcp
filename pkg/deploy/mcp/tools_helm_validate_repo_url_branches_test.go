package mcp

import (
	"errors"
	"strings"
	"testing"
)

// validateHelmRepoURL branch coverage was 87.5% because two DNS-branch arms
// had no coverage:
//
//  1. helmHostResolver returning a non-nil error (tools_helm.go:123–124).
//     Existing tests always stub the resolver to succeed.
//
//  2. helmHostResolver returning a slice with a value that net.ParseIP
//     rejects (tools_helm.go:128 — the `if ip == nil { continue }` skip).
//     Existing tests always return well-formed IP literals.
//
// Both arms are trivially reachable via the existing setHelmMockResolver
// helper; they were simply never exercised.

func TestValidateHelmRepoURL_DNSResolutionError(t *testing.T) {
	setHelmMockResolver(t, func(host string) ([]string, error) {
		return nil, errors.New("simulated NXDOMAIN")
	})
	err := validateHelmRepoURL("https://unresolvable.example.invalid/charts")
	if err == nil {
		t.Fatal("expected error when helmHostResolver fails, got nil")
	}
	// Guard against a future refactor that swallows the resolver error into
	// a generic "blocked" wording — the message must clearly say the host
	// could not be resolved so operators can diagnose transient DNS issues
	// separately from real SSRF blocks.
	if !strings.Contains(err.Error(), "could not be resolved") {
		t.Errorf("expected 'could not be resolved' in error, got %q", err.Error())
	}
}

func TestValidateHelmRepoURL_ResolverEmitsNonIPStringIsSkipped(t *testing.T) {
	// Return one garbage non-IP entry followed by one public IP. The
	// non-IP entry must fall through the `ip == nil { continue }` skip;
	// the public IP must be inspected and pass. If the skip is ever
	// removed and the code trips over the malformed entry, this test
	// will start returning a non-nil error and fail.
	setHelmMockResolver(t, func(host string) ([]string, error) {
		return []string{"not-an-ip-address", "93.184.216.34"}, nil
	})
	if err := validateHelmRepoURL("https://mixed.example.com/charts"); err != nil {
		t.Errorf("expected nil error when one resolver entry is a non-IP string but a public IP follows, got %v", err)
	}
}
