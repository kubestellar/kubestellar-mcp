package server

import (
	"testing"
)

// FuzzValidateNamespace exercises ValidateNamespace with arbitrary byte
// sequences so the Go fuzzing engine can discover inputs that trigger panics
// or unexpected behaviour in the namespace validation logic.
//
// Run with: go test -fuzz=FuzzValidateNamespace ./pkg/mcp/server/
func FuzzValidateNamespace(f *testing.F) {
	// Seed corpus: valid, blocked, and boundary-condition namespaces.
	seeds := []string{
		"default",
		"my-app",
		"kube-system",
		"kube-public",
		"kube-node-lease",
		"gatekeeper-system",
		"openshift",
		"openshift-ingress",
		"",
		"-",
		"a",
		"0",
		"abc-123",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, ns string) {
		// The function must not panic for any input.
		// We do not assert error/no-error; we only assert no panic.
		_ = ValidateNamespace(ns) //nolint:errcheck
	})
}

// FuzzExtractAndValidateNamespace exercises the argument-map helper with
// arbitrary namespace strings injected via the "namespace" key.
func FuzzExtractAndValidateNamespace(f *testing.F) {
	seeds := []string{
		"default",
		"kube-system",
		"",
		"../etc",
		"foo\x00bar",
		"ns;echo pwned",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, ns string) {
		args := map[string]interface{}{"namespace": ns}
		_, _ = extractAndValidateNamespace(args) //nolint:errcheck
	})
}
