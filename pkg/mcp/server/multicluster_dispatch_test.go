package server

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"

	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/kubestellar/kubestellar-mcp/pkg/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// executeMultiCluster is a two-arm dispatcher (see multicluster.go:27):
//
//	if clusterName != "" { return s.executeSingle(...) }
//	return s.executeAll(...)
//
// TestExecuteSingleClusterVariants exercises arm A ("alpha") in the same file.
// Arm B — the empty-clusterName case, used by every MCP tool call that fans
// out across all discovered clusters — has no direct dispatcher-level test
// (executeAll itself is exercised, but only via direct calls that bypass the
// dispatcher). These tests close that gap. Tracked in
// kubestellar/kubestellar-mcp#629.

func TestExecuteMultiClusterDispatchesToExecuteAllOnEmptyClusterName(t *testing.T) {
	// Assert that clusterName == "" (a) invokes the discoverer with source
	// "all" (the executeAll contract), (b) returns one ClusterResult per
	// discovered cluster, and (c) invokes the ExecuteFunc with the right
	// cluster name for each fan-out.
	discoverSource := ""
	var mu sync.Mutex
	execCalls := map[string]int{}

	s := &Server{
		discoverer: stubDiscoverer{discoverClusters: func(source string) ([]cluster.ClusterInfo, error) {
			discoverSource = source
			return []cluster.ClusterInfo{{Name: "alpha"}, {Name: "beta"}}, nil
		}},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(), nil
		},
	}

	results, err := s.executeMultiCluster(context.Background(), "", func(ctx context.Context, client kubernetes.Interface, clusterName string) (interface{}, error) {
		mu.Lock()
		execCalls[clusterName]++
		mu.Unlock()
		return clusterName + "-ok", nil
	})

	require.NoError(t, err)
	assert.Equal(t, "all", discoverSource, "empty-clusterName dispatch must ask the discoverer for source=all")
	require.Len(t, results, 2, "empty-clusterName dispatch must return one ClusterResult per discovered cluster")

	names := []string{results[0].Cluster, results[1].Cluster}
	sort.Strings(names)
	assert.Equal(t, []string{"alpha", "beta"}, names)
	assert.Equal(t, map[string]int{"alpha": 1, "beta": 1}, execCalls,
		"the ExecuteFunc must be invoked exactly once per discovered cluster with the right name")

	for _, r := range results {
		assert.Empty(t, r.Error, "expected no per-cluster errors: got %q on %q", r.Error, r.Cluster)
		assert.Equal(t, r.Cluster+"-ok", r.Result)
	}
}

func TestExecuteMultiClusterEmptyClusterNameSurfacesDiscoveryError(t *testing.T) {
	// Arm-B semantic contract that differs from arm A: a discovery error is
	// returned as a top-level (results, err) error, not folded into a single
	// ClusterResult{Error: ...}. Arm A does the fold because the "single
	// cluster" scope makes the identity of the offending cluster obvious;
	// arm B has no such scope, so it must surface the failure.
	sentinel := errors.New("discovery boom")
	s := &Server{
		discoverer: stubDiscoverer{discoverClusters: func(source string) ([]cluster.ClusterInfo, error) {
			return nil, sentinel
		}},
	}

	results, err := s.executeMultiCluster(context.Background(), "", func(ctx context.Context, client kubernetes.Interface, clusterName string) (interface{}, error) {
		t.Fatal("ExecuteFunc must not be invoked when discovery fails")
		return nil, nil
	})

	require.Error(t, err)
	require.Nil(t, results)
	assert.ErrorIs(t, err, sentinel, "discovery error must reach the caller (wrapped is fine, but must chain)")
}

func TestExecuteMultiClusterEmptyClusterNameFailsOnEmptyDiscovery(t *testing.T) {
	// A distinct arm-B failure mode: the discoverer succeeded but returned
	// zero clusters. executeAll treats this as a top-level error (see
	// multicluster.go:69). Pinning it here so a future rewrite that
	// silently returns []ClusterResult{} would fail this test loudly.
	s := &Server{
		discoverer: stubDiscoverer{discoverClusters: func(source string) ([]cluster.ClusterInfo, error) {
			return nil, nil
		}},
	}

	results, err := s.executeMultiCluster(context.Background(), "", func(ctx context.Context, client kubernetes.Interface, clusterName string) (interface{}, error) {
		t.Fatal("ExecuteFunc must not be invoked when no clusters were discovered")
		return nil, nil
	})

	require.Error(t, err)
	require.Nil(t, results)
	assert.Contains(t, err.Error(), "no clusters found")
}
