package kubectl

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleKubectlApplyBlocksSensitiveKindVariantsInManifest(t *testing.T) {
	cases := []struct {
		name     string
		manifest string
		kindMsg  string
	}{
		{name: "yaml ClusterRole", manifest: "apiVersion: rbac.authorization.k8s.io/v1\nkind: ClusterRole\nmetadata:\n  name: cluster-admin-escalate\n", kindMsg: "ClusterRole"},
		{name: "yaml ClusterRoleBinding", manifest: "apiVersion: rbac.authorization.k8s.io/v1\nkind: ClusterRoleBinding\nmetadata:\n  name: escalate\n", kindMsg: "ClusterRoleBinding"},
		{name: "yaml ServiceAccount", manifest: "apiVersion: v1\nkind: ServiceAccount\nmetadata:\n  name: priv-sa\n  namespace: default\n", kindMsg: "ServiceAccount"},
		{name: "json Secret", manifest: `{"apiVersion":"v1","kind":"Secret","metadata":{"name":"creds"}}`, kindMsg: "Secret"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := HandleKubectlApply(context.Background(), newFakeDeps().deps(), mustMarshalJSON(t, map[string]interface{}{
				"manifest": tc.manifest,
				"clusters": []string{"alpha"},
				"dry_run":  true,
			}))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.kindMsg)
			assert.Contains(t, err.Error(), "blocked")
			assert.Contains(t, err.Error(), "kubectl directly")
		})
	}
}

func TestHandleDeleteResourceBlocksSensitiveKindVariants(t *testing.T) {
	cases := []string{"ClusterRole", "ClusterRoleBinding", "ServiceAccount", "secret", "clusterrole"}
	for _, kind := range cases {
		t.Run(kind, func(t *testing.T) {
			_, err := HandleDeleteResource(context.Background(), newFakeDeps().deps(), mustMarshalJSON(t, map[string]interface{}{
				"kind":     kind,
				"name":     "target",
				"clusters": []string{"alpha"},
				"dry_run":  true,
			}))
			require.Error(t, err)
			assert.Contains(t, err.Error(), kind)
			assert.Contains(t, err.Error(), "blocked")
		})
	}
}
