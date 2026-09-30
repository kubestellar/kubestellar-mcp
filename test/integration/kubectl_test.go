//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/kubectl"
	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
)

// integrationClusterName is the single synthetic cluster name this suite
// binds to the shared envtest apiserver.
const integrationClusterName = "envtest"

// newKubectlDeps wires kubectl.Deps directly to the shared envtest
// environment, mirroring how pkg/deploy/mcp/kubectl_adapter.go wires the
// production *Server's manager/executor, but with a single hard-coded
// cluster instead of a multi-cluster kubeconfig.
func newKubectlDeps(t *testing.T) kubectl.Deps {
	t.Helper()

	return kubectl.Deps{
		DiscoverClusters: func() ([]multicluster.ClusterInfo, error) {
			return []multicluster.ClusterInfo{{Name: integrationClusterName}}, nil
		},
		ExecuteOnSelected: func(ctx context.Context, clusterNames []string, fn multicluster.ExecuteFunc) ([]multicluster.ClusterResult, error) {
			clientset, err := kubernetes.NewForConfig(testCfg)
			if err != nil {
				t.Fatalf("kubernetes.NewForConfig: %v", err)
			}
			results := make([]multicluster.ClusterResult, 0, len(clusterNames))
			for _, name := range clusterNames {
				result, err := fn(ctx, clientset, name)
				if err != nil {
					results = append(results, multicluster.ClusterResult{Cluster: name, Error: err.Error()})
					continue
				}
				results = append(results, multicluster.ClusterResult{Cluster: name, Result: result})
			}
			return results, nil
		},
		GetConfig: func(clusterName string) (*rest.Config, error) {
			return testCfg, nil
		},
		IsSensitiveKind: func(kind string) bool {
			// ConfigMap (the only kind this suite applies/deletes) is never
			// sensitive; the sensitive-kind gate itself is covered by unit
			// tests in pkg/deploy/mcp/kubectl.
			return false
		},
		SensitiveKindError: func(kind string) error {
			return nil
		},
		ManifestSensitiveKind: func(doc string) (string, bool) {
			return "", false
		},
		IsNamespaceKind: func(kind string) bool {
			switch strings.ToLower(kind) {
			case "namespace", "namespaces", "ns":
				return true
			default:
				return false
			}
		},
		YAMLToJSON: func(y string) string {
			data, err := k8syaml.ToJSON([]byte(y))
			if err != nil {
				return y
			}
			return string(data)
		},
		UnstructuredFromYAML: func(y string, obj *unstructured.Unstructured) error {
			data, err := k8syaml.ToJSON([]byte(y))
			if err != nil {
				return err
			}
			return json.Unmarshal(data, obj)
		},
	}
}

// TestKubectlApplyDeleteRoundTrip exercises pkg/deploy/mcp/kubectl's
// kubectl_apply and delete_resource MCP tools end-to-end against a real
// kube-apiserver (envtest): apply creates a ConfigMap, a second apply
// updates it in place, delete removes it, and a second delete reports
// not-found. This is the real HTTP round-trip that
// pkg/deploy/mcp/kubectl's own unit tests (apply_dynamic_test.go,
// delete_test.go) fake out with httptest/fake clientsets, so it is the
// concrete evidence kubestellar-mcp#1063 asked for that dynamic-GVR
// resolution and the apply/delete handlers work against a real API server.
func TestKubectlApplyDeleteRoundTrip(t *testing.T) {
	ctx := context.Background()
	d := newKubectlDeps(t)

	const manifest = `
apiVersion: v1
kind: ConfigMap
metadata:
  name: mcp-integration-cm
  namespace: default
data:
  hello: world
`

	// Apply #1: expect a create.
	applyArgs, err := json.Marshal(map[string]interface{}{
		"manifest": manifest,
		"clusters": []string{integrationClusterName},
	})
	if err != nil {
		t.Fatalf("json.Marshal(apply args): %v", err)
	}

	res, err := kubectl.HandleKubectlApply(ctx, d, applyArgs)
	if err != nil {
		t.Fatalf("HandleKubectlApply (create): %v", err)
	}
	status := firstApplyStatus(t, res)
	if status != "created" {
		t.Fatalf("first apply: expected status=created, got %q (full result: %+v)", status, res)
	}

	// Apply #2: same manifest, expect an update (the object already exists).
	res, err = kubectl.HandleKubectlApply(ctx, d, applyArgs)
	if err != nil {
		t.Fatalf("HandleKubectlApply (update): %v", err)
	}
	status = firstApplyStatus(t, res)
	if status != "updated" {
		t.Fatalf("second apply: expected status=updated, got %q (full result: %+v)", status, res)
	}

	// Confirm the object is really there via a direct client read.
	clientset, err := kubernetes.NewForConfig(testCfg)
	if err != nil {
		t.Fatalf("kubernetes.NewForConfig: %v", err)
	}
	if _, err := clientset.CoreV1().ConfigMaps("default").Get(ctx, "mcp-integration-cm", metav1.GetOptions{}); err != nil {
		t.Fatalf("expected ConfigMap to exist after apply, Get failed: %v", err)
	}

	// Delete #1: expect "deleted".
	deleteArgs, err := json.Marshal(map[string]interface{}{
		"kind":     "ConfigMap",
		"name":     "mcp-integration-cm",
		"clusters": []string{integrationClusterName},
	})
	if err != nil {
		t.Fatalf("json.Marshal(delete args): %v", err)
	}

	res, err = kubectl.HandleDeleteResource(ctx, d, deleteArgs)
	if err != nil {
		t.Fatalf("HandleDeleteResource (delete): %v", err)
	}
	status = firstDeleteStatus(t, res)
	if status != "deleted" {
		t.Fatalf("first delete: expected status=deleted, got %q (full result: %+v)", status, res)
	}

	// Delete #2: object is already gone, expect "not-found".
	res, err = kubectl.HandleDeleteResource(ctx, d, deleteArgs)
	if err != nil {
		t.Fatalf("HandleDeleteResource (re-delete): %v", err)
	}
	status = firstDeleteStatus(t, res)
	if status != "not-found" {
		t.Fatalf("second delete: expected status=not-found, got %q (full result: %+v)", status, res)
	}
}

func firstApplyStatus(t *testing.T, res interface{}) string {
	t.Helper()
	m, ok := res.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{} result, got %T", res)
	}
	results, ok := m["results"].([]kubectl.ApplyResult)
	if !ok || len(results) == 0 {
		t.Fatalf("expected non-empty []kubectl.ApplyResult in results, got %#v", m["results"])
	}
	return results[0].Status
}

func firstDeleteStatus(t *testing.T, res interface{}) string {
	t.Helper()
	m, ok := res.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{} result, got %T", res)
	}
	results, ok := m["results"].([]kubectl.DeleteResult)
	if !ok || len(results) == 0 {
		t.Fatalf("expected non-empty []kubectl.DeleteResult in results, got %#v", m["results"])
	}
	return results[0].Status
}
