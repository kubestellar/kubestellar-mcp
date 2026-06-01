package server

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"

	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/kubestellar/kubestellar-mcp/pkg/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecuteSingleClusterVariants(t *testing.T) {
	tests := []struct {
		name   string
		server *Server
		execFn ExecuteFunc
		want   ClusterResult
	}{
		{
			name: "successful result",
			server: &Server{clientFactory: func(clusterName string) (kubernetes.Interface, error) {
				require.Equal(t, "alpha", clusterName)
				return k8sfake.NewSimpleClientset(), nil
			}},
			execFn: func(ctx context.Context, client kubernetes.Interface, clusterName string) (interface{}, error) {
				return fmt.Sprintf("%s-ok", clusterName), nil
			},
			want: ClusterResult{Cluster: "alpha", Result: "alpha-ok"},
		},
		{
			name: "client creation error becomes result error",
			server: &Server{clientFactory: func(clusterName string) (kubernetes.Interface, error) {
				return nil, errors.New("client boom")
			}},
			execFn: func(ctx context.Context, client kubernetes.Interface, clusterName string) (interface{}, error) {
				return nil, nil
			},
			want: ClusterResult{Cluster: "alpha", Error: "client boom"},
		},
		{
			name: "execution error becomes result error",
			server: &Server{clientFactory: func(clusterName string) (kubernetes.Interface, error) {
				return k8sfake.NewSimpleClientset(), nil
			}},
			execFn: func(ctx context.Context, client kubernetes.Interface, clusterName string) (interface{}, error) {
				return nil, errors.New("query failed")
			},
			want: ClusterResult{Cluster: "alpha", Error: "query failed"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := tt.server.executeMultiCluster(context.Background(), "alpha", tt.execFn)
			require.NoError(t, err)
			require.Len(t, results, 1)
			assert.Equal(t, tt.want, results[0])
		})
	}
}

func TestExecuteAllAggregatesResultsAndErrors(t *testing.T) {
	s := &Server{
		discoverer: stubDiscoverer{discoverClusters: func(source string) ([]cluster.ClusterInfo, error) {
			require.Equal(t, "all", source)
			return []cluster.ClusterInfo{{Name: "alpha"}, {Name: "beta"}, {Name: "gamma"}}, nil
		}},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			if clusterName == "beta" {
				return nil, errors.New("client unavailable")
			}
			return k8sfake.NewSimpleClientset(), nil
		},
	}

	results, err := s.executeAll(context.Background(), func(ctx context.Context, client kubernetes.Interface, clusterName string) (interface{}, error) {
		if clusterName == "gamma" {
			return nil, errors.New("fan-out failed")
		}
		return map[string]string{"status": clusterName + "-ok"}, nil
	})
	require.NoError(t, err)
	require.Len(t, results, 3)

	sort.Slice(results, func(i, j int) bool { return results[i].Cluster < results[j].Cluster })
	assert.Equal(t, "alpha", results[0].Cluster)
	assert.Equal(t, map[string]string{"status": "alpha-ok"}, results[0].Result)
	assert.Equal(t, ClusterResult{Cluster: "beta", Error: "client unavailable"}, results[1])
	assert.Equal(t, ClusterResult{Cluster: "gamma", Error: "fan-out failed"}, results[2])
}

func TestExecuteAllDiscoveryFailures(t *testing.T) {
	tests := []struct {
		name      string
		discover  func(string) ([]cluster.ClusterInfo, error)
		wantError string
	}{
		{
			name: "discovery error",
			discover: func(string) ([]cluster.ClusterInfo, error) {
				return nil, errors.New("discover failed")
			},
			wantError: "failed to discover clusters: discover failed",
		},
		{
			name: "no clusters found",
			discover: func(source string) ([]cluster.ClusterInfo, error) {
				require.Equal(t, "all", source)
				return []cluster.ClusterInfo{}, nil
			},
			wantError: "no clusters found from any discovery source",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Server{discoverer: stubDiscoverer{discoverClusters: tt.discover}}
			results, err := s.executeAll(context.Background(), func(ctx context.Context, client kubernetes.Interface, clusterName string) (interface{}, error) {
				return nil, nil
			})
			assert.Nil(t, results)
			require.EqualError(t, err, tt.wantError)
		})
	}
}
