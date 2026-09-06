package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
)

// nodesHandlerServer returns an httptest server that serves a single ready
// node on GET /api/v1/nodes. This lets the single-cluster arm of
// handleListClusterCapabilities exercise the executor closure end-to-end,
// including selector.GetCapabilitiesForCluster.
func nodesHandlerServer(t *testing.T) *httptest.Server {
	t.Helper()
	node := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "n1",
			Labels: map[string]string{"topology.kubernetes.io/region": "us-east-1"},
		},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			},
			Conditions: []corev1.NodeCondition{{
				Type:   corev1.NodeReady,
				Status: corev1.ConditionTrue,
			}},
		},
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/nodes" {
			http.NotFound(w, r)
			return
		}
		list := corev1.NodeList{
			TypeMeta: metav1.TypeMeta{Kind: "NodeList", APIVersion: "v1"},
			Items:    []corev1.Node{node},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(&list)
	}))
}

// TestHandleListClusterCapabilities_SingleClusterSuccess exercises the
// success arm of the single-cluster branch: executor.Execute reaches a live
// (httptest-backed) apiserver, the closure invokes
// selector.GetCapabilitiesForCluster, and results[0].Result is returned to
// the caller. Previously only the params-marshal, unknown-cluster and
// all-clusters arms were covered.
func TestHandleListClusterCapabilities_SingleClusterSuccess(t *testing.T) {
	srv := nodesHandlerServer(t)
	defer srv.Close()

	server := newHelmTestServer(t, map[string]string{"alpha": srv.URL})

	got, err := server.handleListClusterCapabilities(
		context.Background(),
		mustMarshalJSON(t, map[string]interface{}{"cluster": "alpha"}),
	)
	require.NoError(t, err)

	caps, ok := got.(*multicluster.ClusterCapabilities)
	require.True(t, ok, "expected *multicluster.ClusterCapabilities, got %T", got)
	assert.Equal(t, "alpha", caps.Cluster)
	assert.Equal(t, 1, caps.NodeCount)
	assert.Equal(t, 1, caps.ReadyNodes)
}

// TestHandleListClusterCapabilities_SingleClusterListNodesError verifies
// that when the executor closure's inner selector call fails (the apiserver
// returns 500 on /api/v1/nodes), executor.Execute reports it as a
// ClusterResult with Error != "", and handleListClusterCapabilities
// surfaces the generic "failed to get capabilities" message. This covers
// the results[0].Error != "" sub-branch reached via a closure failure
// (previously only reached via GetClient failure on an unknown cluster).
func TestHandleListClusterCapabilities_SingleClusterListNodesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	server := newHelmTestServer(t, map[string]string{"alpha": srv.URL})

	_, err := server.handleListClusterCapabilities(
		context.Background(),
		mustMarshalJSON(t, map[string]interface{}{"cluster": "alpha"}),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get capabilities for cluster alpha")
}
