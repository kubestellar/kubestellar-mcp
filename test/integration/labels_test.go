//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/labels"
	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
)

// integrationLabelsDeps wires labels.Deps directly to the shared envtest
// environment with a single hard-coded cluster, mirroring newKubectlDeps in
// kubectl_test.go (both packages sit under pkg/deploy/mcp and share the
// same multicluster.Deps-style wiring rather than the
// pkg/mcp/server/handlers.Registry pattern used by workloads_test.go/
// rbac_test.go).
type integrationLabelsDeps struct {
	clientset *kubernetes.Clientset
}

func (d *integrationLabelsDeps) DiscoverClusterNames() ([]string, error) {
	return []string{integrationClusterName}, nil
}

func (d *integrationLabelsDeps) ExecuteOnSelected(ctx context.Context, clusterNames []string, fn multicluster.ExecuteFunc) ([]multicluster.ClusterResult, error) {
	results := make([]multicluster.ClusterResult, 0, len(clusterNames))
	for _, name := range clusterNames {
		result, err := fn(ctx, d.clientset, name)
		if err != nil {
			results = append(results, multicluster.ClusterResult{Cluster: name, Error: err.Error()})
			continue
		}
		results = append(results, multicluster.ClusterResult{Cluster: name, Result: result})
	}
	return results, nil
}

// IsSensitiveKind always reports false: the sensitive-kind gate itself is
// covered by pkg/deploy/mcp/labels's own unit tests
// (TestLabelOperationsBlockSensitiveKinds), so this suite only needs the
// gate to stay out of the way of the ConfigMap round-trip it exercises.
func (d *integrationLabelsDeps) IsSensitiveKind(kind string) bool {
	return false
}

func (d *integrationLabelsDeps) SensitiveKindError(kind string) error {
	return nil
}

func newLabelsDeps(t *testing.T) *integrationLabelsDeps {
	t.Helper()
	clientset, err := kubernetes.NewForConfig(testCfg)
	require.NoError(t, err, "kubernetes.NewForConfig")
	return &integrationLabelsDeps{clientset: clientset}
}

// labelsHandlerResult unmarshals the map[string]interface{} that
// HandleAddLabels/HandleRemoveLabels return into just the per-cluster
// results slice, so tests can assert on the first (and only) result's
// status/message the same way a real MCP client would see it serialized.
func labelsHandlerResult(t *testing.T, res interface{}) []labels.LabelResult {
	t.Helper()
	m, ok := res.(map[string]interface{})
	require.Truef(t, ok, "expected map[string]interface{} result, got %T", res)
	results, ok := m["results"].([]labels.LabelResult)
	require.Truef(t, ok, "expected []labels.LabelResult in results, got %#v", m["results"])
	return results
}

// TestLabelsAddRemoveRoundTrip exercises pkg/deploy/mcp/labels's
// add_labels/remove_labels MCP tools end-to-end against a real
// kube-apiserver (envtest): a ConfigMap is seeded directly via the typed
// clientset, add_labels applies a label and the test confirms it on both
// the tool's own string/struct output and a direct client Get, then
// remove_labels strips it back off and the test confirms the label is gone
// from the live object. A not-found case (label a resource that was never
// created) is covered too, mirroring TestKubectlApplyDeleteRoundTrip's
// create/update/delete/re-delete shape for the labels domain's
// add/remove/not-found shape. This is the real HTTP round-trip that the
// package's own unit tests (labels_test.go, labels_multicluster_test.go)
// fake out with httptest servers and fake clientsets.
func TestLabelsAddRemoveRoundTrip(t *testing.T) {
	ctx := context.Background()
	d := newLabelsDeps(t)

	const namespace = "mcp-integration-labels"
	const configMapName = "mcp-integration-labels-cm"

	if _, err := d.clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create namespace: %v", err)
	}

	if _, err := d.clientset.CoreV1().ConfigMaps(namespace).Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: configMapName},
		Data:       map[string]string{"hello": "world"},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create configmap: %v", err)
	}

	t.Run("add_labels labels the live object", func(t *testing.T) {
		args, err := json.Marshal(map[string]interface{}{
			"kind":      "ConfigMap",
			"name":      configMapName,
			"namespace": namespace,
			"labels":    map[string]string{"team": "platform"},
			"clusters":  []string{integrationClusterName},
		})
		require.NoError(t, err, "json.Marshal(add_labels args)")

		res, err := labels.HandleAddLabels(ctx, d, args)
		require.NoError(t, err, "HandleAddLabels")

		results := labelsHandlerResult(t, res)
		require.Len(t, results, 1, "expected exactly one per-cluster result")
		require.Equal(t, "labeled", results[0].Status, "add_labels status")
		require.Equal(t, "platform", results[0].Labels["team"], "add_labels result.Labels")

		cm, err := d.clientset.CoreV1().ConfigMaps(namespace).Get(ctx, configMapName, metav1.GetOptions{})
		require.NoError(t, err, "Get configmap after add_labels")
		require.Equal(t, "platform", cm.Labels["team"], "live ConfigMap label after add_labels")
	})

	t.Run("remove_labels removes the label from the live object", func(t *testing.T) {
		args, err := json.Marshal(map[string]interface{}{
			"kind":      "ConfigMap",
			"name":      configMapName,
			"namespace": namespace,
			"labels":    []string{"team"},
			"clusters":  []string{integrationClusterName},
		})
		require.NoError(t, err, "json.Marshal(remove_labels args)")

		res, err := labels.HandleRemoveLabels(ctx, d, args)
		require.NoError(t, err, "HandleRemoveLabels")

		results := labelsHandlerResult(t, res)
		require.Len(t, results, 1, "expected exactly one per-cluster result")
		require.Equal(t, "unlabeled", results[0].Status, "remove_labels status")

		cm, err := d.clientset.CoreV1().ConfigMaps(namespace).Get(ctx, configMapName, metav1.GetOptions{})
		require.NoError(t, err, "Get configmap after remove_labels")
		_, present := cm.Labels["team"]
		require.False(t, present, "expected label %q to be removed from live ConfigMap, got labels=%v", "team", cm.Labels)
	})

	t.Run("add_labels reports not-found for a missing resource", func(t *testing.T) {
		args, err := json.Marshal(map[string]interface{}{
			"kind":      "ConfigMap",
			"name":      "mcp-integration-labels-missing",
			"namespace": namespace,
			"labels":    map[string]string{"team": "platform"},
			"clusters":  []string{integrationClusterName},
		})
		require.NoError(t, err, "json.Marshal(add_labels args, missing resource)")

		res, err := labels.HandleAddLabels(ctx, d, args)
		require.NoError(t, err, "HandleAddLabels (missing resource)")

		results := labelsHandlerResult(t, res)
		require.Len(t, results, 1, "expected exactly one per-cluster result")
		require.Equal(t, "not-found", results[0].Status, "add_labels status for a resource that does not exist")
	})
}
