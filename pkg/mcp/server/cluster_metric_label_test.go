package server

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kubestellar/kubestellar-mcp/pkg/cluster"
)

// TestClusterMetricLabelBoundsArbitraryInput verifies that clusterMetricLabel
// never forwards an arbitrary caller-supplied "cluster" argument straight
// into a metric label: it must either match a name/context the discoverer
// reported, or collapse to the closed unrecognizedClusterLabel sentinel.
func TestClusterMetricLabelBoundsArbitraryInput(t *testing.T) {
	s := &Server{
		discoverer: stubDiscoverer{
			discoverClusters: func(string) ([]cluster.ClusterInfo, error) {
				return []cluster.ClusterInfo{
					{Name: "prod", Context: "prod-ctx"},
				}, nil
			},
		},
	}

	tests := []struct {
		name string
		args map[string]interface{}
		want string
	}{
		{"absent cluster arg", map[string]interface{}{}, ""},
		{"known name", map[string]interface{}{"cluster": "prod"}, "prod"},
		{"known context", map[string]interface{}{"cluster": "prod-ctx"}, "prod-ctx"},
		{"unrecognized value", map[string]interface{}{"cluster": "totally-made-up-cluster-name"}, unrecognizedClusterLabel},
		{"non-string value", map[string]interface{}{"cluster": 123}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, s.clusterMetricLabel(tt.args))
		})
	}
}

// TestClusterMetricLabelNilDiscovererIsBounded covers the defensive path
// where no discoverer is configured (e.g. a bare &Server{} in tests): the
// label must still collapse to the closed sentinel rather than passing the
// raw argument through.
func TestClusterMetricLabelNilDiscovererIsBounded(t *testing.T) {
	s := &Server{}
	assert.Equal(t, unrecognizedClusterLabel, s.clusterMetricLabel(map[string]interface{}{"cluster": "anything"}))
	assert.Empty(t, s.clusterMetricLabel(map[string]interface{}{}))
}

// TestClusterMetricLabelCachesAndSurvivesDiscoveryErrors covers the TTL
// cache: repeated calls within clusterLabelCacheTTL must not re-invoke the
// discoverer, and a transient discovery error must not wipe a previously
// cached, still-valid set.
func TestClusterMetricLabelCachesAndSurvivesDiscoveryErrors(t *testing.T) {
	calls := 0
	s := &Server{
		discoverer: stubDiscoverer{
			discoverClusters: func(string) ([]cluster.ClusterInfo, error) {
				calls++
				if calls > 1 {
					return nil, errors.New("transient discovery failure")
				}
				return []cluster.ClusterInfo{{Name: "staging"}}, nil
			},
		},
	}

	assert.Equal(t, "staging", s.clusterMetricLabel(map[string]interface{}{"cluster": "staging"}))
	assert.Equal(t, 1, calls, "first lookup should hit the discoverer once")

	// Still within the TTL: cached set is reused, discoverer not called again.
	assert.Equal(t, "staging", s.clusterMetricLabel(map[string]interface{}{"cluster": "staging"}))
	assert.Equal(t, 1, calls, "second lookup within TTL should reuse the cache")

	// Force the cache to look stale, then confirm a discovery error falls
	// back to the previously cached set instead of unrecognizing everything.
	s.clusterLabelMu.Lock()
	s.clusterLabelCachedAt = time.Now().Add(-2 * clusterLabelCacheTTL)
	s.clusterLabelMu.Unlock()

	assert.Equal(t, "staging", s.clusterMetricLabel(map[string]interface{}{"cluster": "staging"}))
	assert.Equal(t, 2, calls, "stale cache should trigger a re-discovery attempt")
}
