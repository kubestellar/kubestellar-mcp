package server

import (
	"errors"
	"testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/cluster"
)

// TestBoundedClusterLabel locks in that a caller-supplied "cluster" tool
// argument is only ever forwarded to metrics/log labels verbatim when it
// matches a cluster this server actually knows about (via kubeconfig
// discovery). Any other client-controlled value must collapse to the fixed
// otherClusterLabel so the label stays bounded regardless of what a caller
// sends.
func TestBoundedClusterLabel(t *testing.T) {
	tests := []struct {
		name    string
		cluster string
		server  *Server
		want    string
	}{
		{
			name:    "empty cluster passes through unchanged",
			cluster: "",
			server:  &Server{discoverer: stubDiscoverer{}},
			want:    "",
		},
		{
			name:    "known cluster is preserved",
			cluster: "alpha",
			server: &Server{discoverer: stubDiscoverer{discoverClusters: func(source string) ([]cluster.ClusterInfo, error) {
				if source != "kubeconfig" {
					t.Fatalf("DiscoverClusters source = %q, want kubeconfig", source)
				}
				return []cluster.ClusterInfo{{Name: "alpha"}, {Name: "beta"}}, nil
			}}},
			want: "alpha",
		},
		{
			name:    "unrecognized client-supplied cluster is bounded to other",
			cluster: "arbitrary-attacker-controlled-string",
			server: &Server{discoverer: stubDiscoverer{discoverClusters: func(source string) ([]cluster.ClusterInfo, error) {
				return []cluster.ClusterInfo{{Name: "alpha"}}, nil
			}}},
			want: otherClusterLabel,
		},
		{
			name:    "discovery error is bounded to other",
			cluster: "alpha",
			server: &Server{discoverer: stubDiscoverer{discoverClusters: func(source string) ([]cluster.ClusterInfo, error) {
				return nil, errors.New("kubeconfig load failed")
			}}},
			want: otherClusterLabel,
		},
		{
			name:    "nil discoverer is bounded to other",
			cluster: "alpha",
			server:  &Server{},
			want:    otherClusterLabel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.server.boundedClusterLabel(tt.cluster); got != tt.want {
				t.Fatalf("boundedClusterLabel(%q) = %q, want %q", tt.cluster, got, tt.want)
			}
		})
	}
}
