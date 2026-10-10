// Package testsupport holds tiny test-only helpers shared across the
// pkg/deploy/mcp/* per-domain sub-packages (app, deploy, gitops, helm,
// kubectl, kustomize, labels) and the root mcp package itself. These
// packages were split out of a single flat package by epic #983; each one
// had independently redefined the same marshal-or-fail test fixture, so this
// package gives them one place to share it instead.
package testsupport

import (
	"encoding/json"
	"testing"
)

// MustMarshalJSON marshals v to JSON and fails the test immediately if
// marshaling errors, returning the encoded bytes as a json.RawMessage.
func MustMarshalJSON(t testing.TB, v interface{}) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return data
}
