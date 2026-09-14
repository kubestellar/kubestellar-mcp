package gitops

import (
	"strings"
	"testing"
)

// TestRevalidateRepoHost_BlocksLoopbackIP asserts that the second-pass
// host revalidation catches an IP literal in a blocked range even if the
// first-pass validator accepted the URL (defense-in-depth against DNS
// rebinding — see issue #884).
func TestRevalidateRepoHost_BlocksLoopbackIP(t *testing.T) {
	err := revalidateRepoHost("https://127.0.0.1/repo.git")
	if err == nil {
		t.Fatalf("expected block for loopback IP, got nil")
	}
	if !strings.Contains(err.Error(), "blocked IP") {
		t.Fatalf("expected blocked-IP error, got %v", err)
	}
}

// TestRevalidateRepoHost_BlocksCloudMetadataIP catches the primary SSRF
// target (169.254.169.254) at the second-pass gate.
func TestRevalidateRepoHost_BlocksCloudMetadataIP(t *testing.T) {
	err := revalidateRepoHost("https://169.254.169.254/repo.git")
	if err == nil {
		t.Fatalf("expected block for cloud metadata IP, got nil")
	}
}

// TestRevalidateRepoHost_SkipsFileScheme confirms local file:// URLs used
// by tests still short-circuit and do not attempt DNS resolution.
func TestRevalidateRepoHost_SkipsFileScheme(t *testing.T) {
	if err := revalidateRepoHost("file:///tmp/repo"); err != nil {
		t.Fatalf("expected nil for file:// scheme, got %v", err)
	}
}

// TestRevalidateRepoHost_EmptyReturnsNil confirms the helper is safe to
// call with an empty repo (matches ReadFromGit's guard style).
func TestRevalidateRepoHost_EmptyReturnsNil(t *testing.T) {
	if err := revalidateRepoHost(""); err != nil {
		t.Fatalf("expected nil for empty repo, got %v", err)
	}
}
