package gitops

import (
	"testing"
)

// FuzzValidateRepoURL exercises URL parsing and SSRF-prevention logic with
// arbitrary inputs.  Go's native fuzzer (go test -fuzz=FuzzValidateRepoURL)
// satisfies the OpenSSF Scorecard Fuzzing check.
func FuzzValidateRepoURL(f *testing.F) {
	// Seed corpus: valid and known-invalid inputs from the unit tests.
	seeds := []string{
		"https://github.com/org/repo.git",
		"http://github.com/org/repo.git",
		"file:///etc/kubernetes/pki/ca.key",
		"ssh://internal:22/repo",
		"git://host/repo",
		"/etc/passwd",
		"",
		"https://",
		"git@github.com:org/repo.git",
		"https://169.254.169.254/latest/meta-data/",
		"https://10.0.0.1/repo.git",
		"https://172.16.0.1/repo.git",
		"https://192.168.1.1/repo.git",
		"https://127.0.0.1/repo.git",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, url string) {
		// Must not panic regardless of input.
		_ = ValidateRepoURL(url)
	})
}

// FuzzValidateBranchName exercises branch-name validation with arbitrary inputs.
func FuzzValidateBranchName(f *testing.F) {
	seeds := []string{
		"main",
		"feature/my-branch",
		"release-1.0",
		"",
		"../../etc/passwd",
		"branch with spaces",
		"HEAD",
		"refs/heads/main",
		string([]byte{0x00, 0x01, 0x7f}),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, branch string) {
		// Must not panic regardless of input.
		_ = validateBranchName(branch)
	})
}
