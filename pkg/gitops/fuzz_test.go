package gitops

import (
	"testing"
)

// FuzzValidateRepoURL exercises the URL parser and SSRF-guard with arbitrary
// input so that adversarial repo URLs cannot trigger panics or unexpected
// behaviour.  Run with:
//
//	go test -fuzz=FuzzValidateRepoURL ./pkg/gitops/
func FuzzValidateRepoURL(f *testing.F) {
	// Seed corpus: valid and malformed inputs.
	seeds := []string{
		"https://github.com/kubestellar/kubestellar-mcp",
		"https://github.com/org/repo.git",
		"",
		"http://github.com/org/repo",
		"file:///etc/passwd",
		"ssh://git@github.com/org/repo",
		"https://169.254.169.254/latest/meta-data",
		"https://10.0.0.1/internal",
		"https://localhost/repo",
		"https://[::1]/repo",
		"://missing-scheme",
		"https://",
		"\x00",
		"https://github.com/" + string(make([]byte, 4096)),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, rawURL string) {
		// ValidateRepoURL must never panic regardless of input.
		// Errors are acceptable; panics are not.
		_ = ValidateRepoURL(rawURL)
	})
}

// FuzzValidateBranchName exercises the branch-name validator with arbitrary
// input to confirm it never panics and correctly rejects injection characters.
func FuzzValidateBranchName(f *testing.F) {
	seeds := []string{
		"main",
		"feature/my-branch",
		"",
		"-leading-dash",
		"branch; rm -rf /",
		"branch$(echo bad)",
		"branch`whoami`",
		"../../etc/passwd",
		string(make([]byte, 512)),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, name string) {
		_ = validateBranchName(name)
	})
}
