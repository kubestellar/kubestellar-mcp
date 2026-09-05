package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandleKubectlApply_BlocksSensitiveKindInManifest verifies the
// sensitive-kind guard at handleKubectlApply (tools_kubectl.go:259-262)
// rejects a manifest whose Kind is on the RBAC/Secret/ServiceAccount
// blocklist BEFORE any cluster fan-out.
//
// The individual kind classifier (isSensitiveKind /
// manifestSensitiveKind) is unit-tested in tools_kubectl_sensitive_test.go,
// but nothing exercises the guard from the entry-point handler — so a
// regression that removed the loop (e.g. accidental refactor of the guard
// out of the top-of-handler flow) would silently let AI-driven
// privilege-escalation manifests through.
func TestHandleKubectlApply_BlocksSensitiveKindInManifest(t *testing.T) {
	cases := []struct {
		name     string
		manifest string
		kindMsg  string
	}{
		{
			name:     "yaml Secret",
			manifest: "apiVersion: v1\nkind: Secret\nmetadata:\n  name: creds\n  namespace: default\n",
			kindMsg:  "Secret",
		},
		{
			name:     "yaml ClusterRole",
			manifest: "apiVersion: rbac.authorization.k8s.io/v1\nkind: ClusterRole\nmetadata:\n  name: cluster-admin-escalate\n",
			kindMsg:  "ClusterRole",
		},
		{
			name:     "yaml ClusterRoleBinding",
			manifest: "apiVersion: rbac.authorization.k8s.io/v1\nkind: ClusterRoleBinding\nmetadata:\n  name: escalate\n",
			kindMsg:  "ClusterRoleBinding",
		},
		{
			name:     "yaml ServiceAccount",
			manifest: "apiVersion: v1\nkind: ServiceAccount\nmetadata:\n  name: priv-sa\n  namespace: default\n",
			kindMsg:  "ServiceAccount",
		},
		{
			name:     "json Secret",
			manifest: `{"apiVersion":"v1","kind":"Secret","metadata":{"name":"creds"}}`,
			kindMsg:  "Secret",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})
			args := mustMarshalJSON(t, map[string]interface{}{
				"manifest": tc.manifest,
				"clusters": []string{"alpha"},
				"dry_run":  true,
			})
			_, err := server.handleKubectlApply(context.Background(), args)
			require.Error(t, err, "sensitive kind %q must be rejected", tc.kindMsg)
			assert.Contains(t, err.Error(), tc.kindMsg)
			assert.Contains(t, err.Error(), "blocked")
			assert.Contains(t, err.Error(), "kubectl directly")
		})
	}
}

// TestHandleKubectlApply_BlocksSensitiveKindInMultiDoc verifies the
// guard fires when a sensitive kind is hidden in a multi-doc manifest
// AFTER an innocent doc. Without walking every document, a caller could
// smuggle a ClusterRole past the check by prepending a benign ConfigMap.
func TestHandleKubectlApply_BlocksSensitiveKindInMultiDoc(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})

	manifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: benign
  namespace: default
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: escalate
`
	args := mustMarshalJSON(t, map[string]interface{}{
		"manifest": manifest,
		"clusters": []string{"alpha"},
		"dry_run":  true,
	})
	_, err := server.handleKubectlApply(context.Background(), args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ClusterRole")
	assert.Contains(t, err.Error(), "blocked")
}

// TestHandleDeleteResource_BlocksSensitiveKind verifies the
// sensitive-kind guard at handleDeleteResource (tools_kubectl.go:101-103).
// Same rationale as the apply guard — the isSensitiveKind classifier is
// unit-tested, but the handler-entry-point guard has no direct coverage.
func TestHandleDeleteResource_BlocksSensitiveKind(t *testing.T) {
	cases := []string{"Secret", "ClusterRole", "ClusterRoleBinding", "ServiceAccount", "secret", "clusterrole"}
	for _, kind := range cases {
		t.Run(kind, func(t *testing.T) {
			server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})
			args := mustMarshalJSON(t, map[string]interface{}{
				"kind":     kind,
				"name":     "target",
				"clusters": []string{"alpha"},
				"dry_run":  true,
			})
			_, err := server.handleDeleteResource(context.Background(), args)
			require.Error(t, err, "delete on sensitive kind %q must be rejected", kind)
			assert.Contains(t, err.Error(), kind)
			assert.Contains(t, err.Error(), "blocked")
		})
	}
}

// TestHandleDeleteResource_RequiresKindAndName covers the
// "kind and name are required" early-return arm — deleting with an empty
// name would fall through to the target-clusters loop and try to remove
// every resource of that kind. Existing tests cover invalid-json but not
// this missing-field guard.
func TestHandleDeleteResource_RequiresKindAndName(t *testing.T) {
	cases := []struct {
		name string
		args map[string]interface{}
	}{
		{name: "missing kind", args: map[string]interface{}{"name": "x"}},
		{name: "missing name", args: map[string]interface{}{"kind": "ConfigMap"}},
		{name: "both empty", args: map[string]interface{}{"kind": "", "name": ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := newHelmTestServer(t, map[string]string{})
			_, err := server.handleDeleteResource(context.Background(), mustMarshalJSON(t, tc.args))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "kind and name are required")
		})
	}
}
