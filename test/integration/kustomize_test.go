//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/kustomize"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/protocol"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/handlers"
	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
)

// newKustomizeRegistry mirrors the server-domain integration tests'
// handlers.Registry shape for pkg/deploy/mcp/kustomize, whose deploy-domain
// registration surface is Deps.Tools().
func newKustomizeRegistry(t *testing.T, deps kustomize.Deps) *handlers.Registry {
	t.Helper()

	reg := handlers.NewRegistry()
	for _, def := range deps.Tools() {
		def := def
		reg.Register(protocol.Tool{
			Name:        def.Name,
			Description: def.Description,
			InputSchema: protocol.InputSchema{
				Type: "object",
			},
		}, func(ctx context.Context, _ *handlers.Deps, args map[string]interface{}) (string, bool) {
			rawArgs, err := json.Marshal(args)
			if err != nil {
				return err.Error(), true
			}

			result, err := def.Handler(ctx, rawArgs)
			if err != nil {
				return err.Error(), true
			}

			data, err := json.Marshal(result)
			if err != nil {
				return err.Error(), true
			}
			return string(data), false
		})
	}
	return reg
}

func newKustomizeDeps() kustomize.Deps {
	return kustomize.Deps{
		DiscoverClusters: func() ([]multicluster.ClusterInfo, error) {
			return []multicluster.ClusterInfo{{Name: integrationClusterName}}, nil
		},
		ValidateClusters: func(clusters []string) error {
			return nil
		},
		ValidateManifest: func(manifest string) error {
			return nil
		},
	}
}

func writeKustomizeFixture(t *testing.T, namespace, name string) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "kustomization.yaml"), []byte(`apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - configmap.yaml
`), 0o600), "write kustomization.yaml")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "configmap.yaml"), []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: `+name+`
  namespace: `+namespace+`
data:
  hello: world
`), 0o600), "write configmap.yaml")

	return dir
}

func kustomizeHandlerResult(t *testing.T, output string) []kustomize.KustomizeResult {
	t.Helper()

	var result struct {
		SuccessCount int                         `json:"successCount"`
		Results      []kustomize.KustomizeResult `json:"results"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &result), "unmarshal kustomize handler output")
	require.Equal(t, 1, result.SuccessCount, "successCount")
	require.Len(t, result.Results, 1, "expected exactly one per-cluster result")
	return result.Results
}

// TestKustomizeApplyDeleteRoundTrip exercises pkg/deploy/mcp/kustomize's
// kustomize_apply/kustomize_delete handlers end-to-end against envtest. It
// points PATH at KUBEBUILDER_ASSETS so the package's fallback from
// `kustomize build` to the real `kubectl kustomize` binary runs, then uses
// the same kubectl binary for apply/delete against the envtest apiserver.
func TestKustomizeApplyDeleteRoundTrip(t *testing.T) {
	ctx := context.Background()

	assets := os.Getenv("KUBEBUILDER_ASSETS")
	require.NotEmpty(t, assets, "KUBEBUILDER_ASSETS must point at setup-envtest assets")
	t.Setenv("PATH", assets)
	t.Setenv("KUBECONFIG", writeIntegrationKubeconfig(t))

	clientset, err := kubernetes.NewForConfig(testCfg)
	require.NoError(t, err, "kubernetes.NewForConfig")

	const namespace = "mcp-integration-kustomize"
	const configMapName = "mcp-integration-kustomize-cm"

	_, err = clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}, metav1.CreateOptions{})
	require.NoError(t, err, "create namespace")

	fixtureDir := writeKustomizeFixture(t, namespace, configMapName)
	reg := newKustomizeRegistry(t, newKustomizeDeps())

	args := map[string]interface{}{
		"path":     fixtureDir,
		"clusters": []string{integrationClusterName},
	}

	tests := []struct {
		name       string
		tool       string
		wantStatus string
		verify     func()
	}{
		{
			name:       "kustomize_apply creates the rendered ConfigMap",
			tool:       "kustomize_apply",
			wantStatus: "applied",
			verify: func() {
				cm, err := clientset.CoreV1().ConfigMaps(namespace).Get(ctx, configMapName, metav1.GetOptions{})
				require.NoError(t, err, "Get configmap after kustomize_apply")
				require.Equal(t, "world", cm.Data["hello"], "live ConfigMap data after kustomize_apply")
			},
		},
		{
			name:       "kustomize_delete removes the rendered ConfigMap",
			tool:       "kustomize_delete",
			wantStatus: "deleted",
			verify: func() {
				_, err := clientset.CoreV1().ConfigMaps(namespace).Get(ctx, configMapName, metav1.GetOptions{})
				require.Truef(t, apierrors.IsNotFound(err), "expected ConfigMap to be deleted, got err=%v", err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := reg.Find(tt.tool)
			require.NotNilf(t, handler, "%s tool not found in registry", tt.tool)

			output, isError := handler(ctx, nil, args)
			require.Falsef(t, isError, "%s returned error output: %s", tt.tool, output)

			results := kustomizeHandlerResult(t, output)
			require.Equal(t, integrationClusterName, results[0].Cluster, "result cluster")
			require.Equal(t, tt.wantStatus, results[0].Status, "result status")
			require.Equal(t, 1, results[0].Resources, "rendered resource count")

			tt.verify()
		})
	}
}
