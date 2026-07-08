package gitops

import (
	"strings"
	"testing"
)

// FuzzValidateRepoURL exercises the URL parser and SSRF-protection logic with
// adversarial inputs.  Run with: go test -fuzz=FuzzValidateRepoURL ./pkg/gitops/
func FuzzValidateRepoURL(f *testing.F) {
	// Seed corpus: valid and known-bad inputs.
	seeds := []string{
		"https://github.com/org/repo.git",
		"http://github.com/org/repo.git",
		"file:///etc/passwd",
		"ssh://internal:22/repo",
		"git://host/repo",
		"",
		"https://",
		"git@github.com:org/repo.git",
		"https://169.254.169.254/latest/meta-data/",
		"https://10.0.0.1/repo.git",
		"https://127.0.0.1/repo.git",
		"https://100.64.0.1/repo.git",
		"https://[::1]/repo.git",
		"https://0x7f000001/repo.git",
		"HtTpS://github.com/org/repo",
		"https://github.com/" + strings.Repeat("a", 512),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		// Must never panic.
		_ = ValidateRepoURL(input)
	})
}

// FuzzValidateBranchName exercises branch-name injection guards with
// adversarial inputs.  Run with: go test -fuzz=FuzzValidateBranchName ./pkg/gitops/
func FuzzValidateBranchName(f *testing.F) {
	seeds := []string{
		"",
		"main",
		"release/v1.2.3",
		"--upload-pack",
		"feature;rm -rf /",
		"feature$(whoami)",
		"../../etc/passwd",
		strings.Repeat("a", 256),
		"refs/heads/main",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		// Must never panic.
		_ = validateBranchName(input)
	})
}

// FuzzReadFromReader exercises the YAML/JSON manifest decoder with adversarial
// payloads.  Run with: go test -fuzz=FuzzReadFromReader ./pkg/gitops/
func FuzzReadFromReader(f *testing.F) {
	seeds := []string{
		// valid Kubernetes-style manifest
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test\n  namespace: default\n",
		// multi-document YAML
		"---\napiVersion: v1\nkind: Pod\n---\napiVersion: v1\nkind: Service\n",
		// empty / null document
		"",
		"---\n",
		"null\n",
		// JSON variant
		`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"x"}}`,
		// deeply nested spec
		"apiVersion: v1\nkind: X\nspec:\n  a:\n    b:\n      c: d\n",
		// unusually long values
		"apiVersion: " + strings.Repeat("x", 1024) + "\nkind: Y\n",
		// binary-ish garbage
		"\x00\x01\x02\x03",
		// YAML anchor / alias bomb (should decode safely without OOM)
		"a: &a []\nb: *a\n",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	r := &ManifestReader{}
	f.Fuzz(func(t *testing.T, input string) {
		// Must never panic; errors are acceptable.
		_, _ = r.ReadFromReader(strings.NewReader(input))
	})
}
