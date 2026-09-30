//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/tools/upgrades"
)

// integrationClusterAccess satisfies upgrades.ClusterAccess by handing back
// typed and dynamic clients built from the shared envtest *rest.Config,
// exactly as the real MCP Server does for a live cluster. This lets the
// Kubernetes-path upgrade tools run their real Discovery().ServerVersion,
// Nodes().List, Pods().List, and dynamic ClusterVersion probes against a real
// apiserver.
type integrationClusterAccess struct {
	clientset     kubernetes.Interface
	dynamicClient dynamic.Interface
}

func (a integrationClusterAccess) GetClientForCluster(string) (kubernetes.Interface, error) {
	return a.clientset, nil
}

func (a integrationClusterAccess) GetDynamicClientForCluster(string) (dynamic.Interface, error) {
	return a.dynamicClient, nil
}

func newUpgradesAccess(t *testing.T) integrationClusterAccess {
	t.Helper()
	clientset, err := kubernetes.NewForConfig(testCfg)
	require.NoError(t, err, "kubernetes.NewForConfig")
	dynamicClient, err := dynamic.NewForConfig(testCfg)
	require.NoError(t, err, "dynamic.NewForConfig")
	return integrationClusterAccess{clientset: clientset, dynamicClient: dynamicClient}
}

// TestUpgradesKubernetesPath exercises pkg/mcp/tools/upgrades's
// detect_cluster_type, get_cluster_version_info, get_upgrade_status, and
// get_upgrade_prerequisites MCP tools end-to-end against a real
// kube-apiserver (envtest). Each tool first probes the OpenShift
// ClusterVersion CRD (absent here, so the real dynamic Get returns
// not-found) and then falls through to its vanilla-Kubernetes branch:
// ServerVersion discovery, node enumeration, and pod-health scanning all run
// against the live apiserver. envtest has no kubelet, so the cluster type
// resolves to "unknown" and node/pod counts are zero — the deterministic
// shape this suite asserts on. The OpenShift-only branches
// (check_olm_operator_upgrades, check_helm_release_upgrades,
// trigger_openshift_upgrade) need CRDs/Helm-release secrets and stay covered
// by the package's own unit tests. This is the real HTTP round-trip those
// unit tests (upgrades_status_test.go, prerequisites_branches_test.go) fake
// with fake clientsets.
func TestUpgradesKubernetesPath(t *testing.T) {
	ctx := context.Background()
	ca := newUpgradesAccess(t)

	tests := []struct {
		name    string
		handler func(ctx context.Context, ca upgrades.ClusterAccess, args map[string]interface{}) (string, bool)
		want    []string
	}{
		{
			name:    "detect_cluster_type",
			handler: upgrades.DetectClusterType,
			want:    []string{"# Cluster Type Detection", "**Kubernetes Version:**", "unknown"},
		},
		{
			name:    "get_cluster_version_info",
			handler: upgrades.GetClusterVersionInfo,
			want:    []string{"# Cluster Version Information", "**Cluster Type:** Kubernetes", "**Current Version:**"},
		},
		{
			name:    "get_upgrade_status",
			handler: upgrades.GetUpgradeStatus,
			want:    []string{"# Upgrade Status", "**Cluster Type:** Kubernetes", "Node Versions"},
		},
		{
			name:    "get_upgrade_prerequisites",
			handler: upgrades.GetUpgradePrerequisites,
			want:    []string{"# Upgrade Prerequisites Check", "Node Health", "Pod Health", "All nodes ready (0/0)"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			output, isError := tt.handler(ctx, ca, map[string]interface{}{
				"cluster": integrationClusterName,
			})
			require.Falsef(t, isError, "%s returned an error result: %s", tt.name, output)
			require.NotEmptyf(t, strings.TrimSpace(output), "%s returned an empty result", tt.name)
			for _, want := range tt.want {
				require.Containsf(t, output, want, "%s output missing %q", tt.name, want)
			}
		})
	}
}
