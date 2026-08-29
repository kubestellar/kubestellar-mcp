package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin behavior of applyManifestDynamic when a multi-document manifest
// contains at least one document whose namespace fails validation
// (system namespace, invalid RFC 1123 label, etc.). They also cover the
// isNamespaceKind branch which validates metadata.name for kind Namespace.
//
// See kubestellar/kubestellar-mcp#626 for the underlying bug:
// applyManifestDynamic returns early with a single-element []ApplyResult on
// mid-stream validation failure, silently discarding results from documents
// processed earlier in the same call. Non-dryRun mode causes cluster drift
// with no audit trail. When #626 lands, the FIXME-marked assertions below
// should flip and force an intentional update.

// TestApplyManifestDynamic_NamespaceKindValidatesName exercises the
// isNamespaceKind branch: for kind: Namespace the value guarded by
// ValidateNamespace is metadata.name (cluster-scoped resource).
func TestApplyManifestDynamic_NamespaceKindValidatesName(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})

	// Valid Namespace kind — should be would-apply in dryRun.
	valid := `apiVersion: v1
kind: Namespace
metadata:
  name: my-app`
	results, err := server.applyManifestDynamic(context.Background(), "alpha", valid, true)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "would-apply", results[0].Status)
	assert.Equal(t, "Namespace", results[0].Kind)
	assert.Equal(t, "my-app", results[0].Name)

	// System Namespace kind (metadata.name = kube-system) — must fail
	// with the metadata.name being validated (not the .metadata.namespace field,
	// which is empty for a cluster-scoped Namespace resource).
	blocked := `apiVersion: v1
kind: Namespace
metadata:
  name: kube-system`
	results, err = server.applyManifestDynamic(context.Background(), "alpha", blocked, true)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "failed", results[0].Status)
	assert.Contains(t, results[0].Message, "invalid namespace in manifest")
	assert.Contains(t, results[0].Message, "kube-system")
}

// TestApplyManifestDynamic_MultiDocMidStreamNamespaceFailure_PinsBug documents
// the buggy behavior described in issue #626: a mid-stream validation failure
// returns a single-element result, discarding both prior and subsequent docs.
//
// FIXME(#626): when the production code is fixed to preserve prior results
// (append + continue, or pre-validate all docs), this test will start failing
// and its assertions must be updated to expect 3 results (2 would-apply +
// 1 failed) in document order.
func TestApplyManifestDynamic_MultiDocMidStreamNamespaceFailure_PinsBug(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})

	manifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: cm1
  namespace: default
data:
  k: v
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm2
  namespace: kube-system
data:
  k: v
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm3
  namespace: default
data:
  k: v`

	results, err := server.applyManifestDynamic(context.Background(), "alpha", manifest, true)
	require.NoError(t, err)

	// FIXME(#626): current buggy behavior — the single failure erases all
	// other results. Once fixed, expect require.Len(t, results, 3) with
	// results[0] = would-apply cm1, results[1] = failed cm2, results[2] =
	// would-apply cm3.
	require.Len(t, results, 1,
		"pinning bug #626: mid-stream validation failure discards prior/next results")
	assert.Equal(t, "failed", results[0].Status)
	assert.Contains(t, results[0].Message, "kube-system")

	// The discarded documents should have been visible to the caller —
	// this assertion documents what is currently lost.
	joined := ""
	for _, r := range results {
		joined += r.Name + ","
	}
	assert.NotContains(t, joined, "cm1",
		"FIXME(#626): cm1 result is silently discarded today")
	assert.NotContains(t, joined, "cm3",
		"FIXME(#626): cm3 result is silently discarded today")
}

// TestApplyManifestDynamic_MultiDocNamespaceKindMidStream_PinsBug is the
// isNamespaceKind variant of the multi-doc mid-stream failure: a bad
// Namespace-kind doc in the middle also truncates results.
//
// FIXME(#626): same disposition as above.
func TestApplyManifestDynamic_MultiDocNamespaceKindMidStream_PinsBug(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})

	manifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: cm-first
  namespace: default
---
apiVersion: v1
kind: Namespace
metadata:
  name: openshift-monitoring
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm-last
  namespace: default`

	results, err := server.applyManifestDynamic(context.Background(), "alpha", manifest, true)
	require.NoError(t, err)

	require.Len(t, results, 1,
		"pinning bug #626: mid-stream Namespace-kind validation failure discards prior/next results")
	assert.Equal(t, "failed", results[0].Status)
	assert.Contains(t, strings.ToLower(results[0].Message), "openshift-monitoring")
}

// TestApplyManifestDynamic_InvalidLabelNamespaceRejected covers the RFC 1123
// label branch of ValidateNamespace via applyManifestDynamic (previously
// only exercised through the blocklist branch).
func TestApplyManifestDynamic_InvalidLabelNamespaceRejected(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})

	// UPPERCASE namespace violates the k8sNamespaceRe regex.
	manifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: cm
  namespace: BAD_NS`

	results, err := server.applyManifestDynamic(context.Background(), "alpha", manifest, true)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "failed", results[0].Status)
	assert.Contains(t, results[0].Message, "invalid namespace in manifest")
}

// TestApplyManifestDynamic_EmptyNamespaceDefaults covers the fallback
// `namespace = "default"` branch that runs when a manifest omits
// metadata.namespace for a namespaced resource.
func TestApplyManifestDynamic_EmptyNamespaceDefaults(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})

	// No metadata.namespace — should default to "default" and pass validation.
	manifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: cm-no-ns
data:
  k: v`

	results, err := server.applyManifestDynamic(context.Background(), "alpha", manifest, true)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "would-apply", results[0].Status)
	assert.Equal(t, "default", results[0].Namespace)
}
