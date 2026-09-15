package gitops

import (
	"strings"
	"testing"
)

// The tests in this file cover branches of revalidateRepoHost that the
// existing manifest_revalidate_test.go leaves uncovered. Prior to this
// file the function measured 47.6% of statements — the loopback IP,
// cloud-metadata IP, file:// scheme, and empty-repo branches were
// exercised, but the URL-parse error, empty-hostname short-circuit,
// non-blocked IP literal, and DNS-failure branches were not. See
// pkg/gitops/manifest.go:116 for the function under test and issue
// #884 for the SSRF / DNS-rebinding context.

// TestRevalidateRepoHost_UnparseableURLRejected reaches the
// `url.Parse` error branch. Go's net/url is permissive, but an unclosed
// IPv6 bracket like "http://[fe80:1/repo" is a documented parse
// failure — that is what this test asserts.
func TestRevalidateRepoHost_UnparseableURLRejected(t *testing.T) {
	err := revalidateRepoHost("http://[fe80:1/repo")
	if err == nil {
		t.Fatalf("expected parse error, got nil")
	}
	if !strings.Contains(err.Error(), "unparseable repo URL") {
		t.Fatalf("expected 'unparseable repo URL' error, got %v", err)
	}
}

// TestRevalidateRepoHost_EmptyHostNonFileSchemeIsNoop covers the
// `u.Hostname() == ""` short-circuit for a non-file scheme. A URL like
// "https:///path" parses but has no host — there is nothing to
// re-resolve, so the helper must return nil rather than fall through
// into DNS lookup.
func TestRevalidateRepoHost_EmptyHostNonFileSchemeIsNoop(t *testing.T) {
	if err := revalidateRepoHost("https:///path"); err != nil {
		t.Fatalf("expected nil for empty host, got %v", err)
	}
}

// TestRevalidateRepoHost_PublicIPLiteralAllowed reaches the
// non-blocked IP-literal path — the branch where net.ParseIP succeeds
// AND isGitopsBlockedIP returns false, so the helper must return nil
// without ever calling the resolver. 8.8.8.8 is a globally routable
// address and is intentionally NOT in any of the blocked ranges (see
// isGitopsBlockedIP at pkg/gitops/manifest.go:41).
func TestRevalidateRepoHost_PublicIPLiteralAllowed(t *testing.T) {
	if err := revalidateRepoHost("https://8.8.8.8/repo.git"); err != nil {
		t.Fatalf("expected nil for public IP literal, got %v", err)
	}
}

// TestRevalidateRepoHost_DNSLookupFailurePropagates covers the
// `net.DefaultResolver.LookupHost` error branch. RFC 6761 reserves the
// `.invalid` TLD as guaranteed not to resolve, so this test does not
// depend on network reachability of any real host — the resolver will
// return NXDOMAIN (or a hostname-lookup error) whether the sandbox has
// egress or not.
func TestRevalidateRepoHost_DNSLookupFailurePropagates(t *testing.T) {
	err := revalidateRepoHost("https://kubestellar-quality-nonexistent.invalid/repo.git")
	if err == nil {
		t.Fatalf("expected DNS lookup failure, got nil")
	}
	if !strings.Contains(err.Error(), "DNS lookup failed") {
		t.Fatalf("expected 'DNS lookup failed' error, got %v", err)
	}
}

// TestRevalidateRepoHost_ResolvedHostBlockedIPRejected covers the final
// uncovered branch: DNS resolution succeeds and at least one returned IP
// is in a blocked range. "localhost" is guaranteed by /etc/hosts (and by
// Go's built-in resolver behaviour) to resolve to 127.0.0.1 (and ::1),
// so this test is deterministic without depending on network egress.
// It hits the loop body at pkg/gitops/manifest.go:150-152 that the
// IP-literal tests cannot reach.
func TestRevalidateRepoHost_ResolvedHostBlockedIPRejected(t *testing.T) {
	err := revalidateRepoHost("https://localhost/repo.git")
	if err == nil {
		t.Fatalf("expected block for hostname resolving to loopback, got nil")
	}
	if !strings.Contains(err.Error(), "blocked IP") {
		t.Fatalf("expected 'blocked IP' error, got %v", err)
	}
}
