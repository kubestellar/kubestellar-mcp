package clusters

import (
	"errors"
	"testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/cluster"
	"github.com/stretchr/testify/require"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

// TestHealthRunFindsNamedCluster covers the branch where a named cluster is
// located in the discovered set and its health is printed.
func TestHealthRunFindsNamedCluster(t *testing.T) {
	oldFactory := newDiscoverer
	t.Cleanup(func() { newDiscoverer = oldFactory })
	newDiscoverer = func(string) clusterDiscoverer {
		return fakeDiscoverer{
			discoverClustersFn: func(source string) ([]cluster.ClusterInfo, error) {
				require.Equal(t, "all", source)
				return []cluster.ClusterInfo{{Name: "other"}, {Name: "prod"}, {Name: "extra"}}, nil
			},
			checkHealthFn: func(info cluster.ClusterInfo) (*cluster.HealthInfo, error) {
				require.Equal(t, "prod", info.Name)
				return &cluster.HealthInfo{
					Status:          "Healthy",
					NodesReady:      "5/5",
					APIServerStatus: "Healthy",
					Message:         "ok",
				}, nil
			},
		}
	}

	o := &healthOptions{configFlags: genericclioptions.NewConfigFlags(true), clusterName: "prod"}
	output, err := captureStdout(t, o.run)
	require.NoError(t, err)
	require.Contains(t, output, "prod")
	require.Contains(t, output, "5/5")
	require.NotContains(t, output, "extra")
}

// TestHealthRunNamedClusterDiscoveryFailure covers the discover-error branch
// taken when a specific cluster name is requested.
func TestHealthRunNamedClusterDiscoveryFailure(t *testing.T) {
	oldFactory := newDiscoverer
	t.Cleanup(func() { newDiscoverer = oldFactory })
	newDiscoverer = func(string) clusterDiscoverer {
		return fakeDiscoverer{discoverClustersFn: func(source string) ([]cluster.ClusterInfo, error) {
			require.Equal(t, "all", source)
			return nil, errors.New("kaboom")
		}}
	}

	o := &healthOptions{configFlags: genericclioptions.NewConfigFlags(true), clusterName: "prod"}
	_, err := captureStdout(t, o.run)
	require.EqualError(t, err, "failed to discover clusters: kaboom")
}

// TestHealthRunCurrentContextDiscoveryFailure covers the discover-error branch
// taken when no --all-clusters flag and no cluster name are supplied (current
// context path).
func TestHealthRunCurrentContextDiscoveryFailure(t *testing.T) {
	oldFactory := newDiscoverer
	t.Cleanup(func() { newDiscoverer = oldFactory })
	newDiscoverer = func(string) clusterDiscoverer {
		return fakeDiscoverer{discoverClustersFn: func(source string) ([]cluster.ClusterInfo, error) {
			require.Equal(t, "kubeconfig", source)
			return nil, errors.New("no kubeconfig")
		}}
	}

	o := &healthOptions{configFlags: genericclioptions.NewConfigFlags(true)}
	_, err := captureStdout(t, o.run)
	require.EqualError(t, err, "failed to discover clusters: no kubeconfig")
}

// TestHealthRunCurrentContextSelectsCurrent covers the else-branch that filters
// discovered clusters down to the one marked Current=true.
func TestHealthRunCurrentContextSelectsCurrent(t *testing.T) {
	oldFactory := newDiscoverer
	t.Cleanup(func() { newDiscoverer = oldFactory })
	newDiscoverer = func(string) clusterDiscoverer {
		return fakeDiscoverer{
			discoverClustersFn: func(source string) ([]cluster.ClusterInfo, error) {
				require.Equal(t, "kubeconfig", source)
				return []cluster.ClusterInfo{
					{Name: "dev", Current: false},
					{Name: "staging", Current: true},
					{Name: "prod", Current: false},
				}, nil
			},
			checkHealthFn: func(info cluster.ClusterInfo) (*cluster.HealthInfo, error) {
				require.Equal(t, "staging", info.Name)
				return &cluster.HealthInfo{Status: "Healthy", NodesReady: "2/2", APIServerStatus: "Healthy", Message: "ok"}, nil
			},
		}
	}

	o := &healthOptions{configFlags: genericclioptions.NewConfigFlags(true)}
	output, err := captureStdout(t, o.run)
	require.NoError(t, err)
	require.Contains(t, output, "staging")
	require.NotContains(t, output, "dev")
	require.NotContains(t, output, "prod")
}

// TestHealthCommandRunEUsesFirstArg exercises the RunE closure of the cobra
// command so the positional-argument assignment path in newHealthCommand runs.
func TestHealthCommandRunEUsesFirstArg(t *testing.T) {
	oldFactory := newDiscoverer
	t.Cleanup(func() { newDiscoverer = oldFactory })
	newDiscoverer = func(string) clusterDiscoverer {
		return fakeDiscoverer{discoverClustersFn: func(source string) ([]cluster.ClusterInfo, error) {
			require.Equal(t, "all", source)
			return []cluster.ClusterInfo{{Name: "target"}}, nil
		}, checkHealthFn: func(info cluster.ClusterInfo) (*cluster.HealthInfo, error) {
			require.Equal(t, "target", info.Name)
			return &cluster.HealthInfo{Status: "Healthy", NodesReady: "1/1", APIServerStatus: "Healthy", Message: "ok"}, nil
		}}
	}

	cmd := newHealthCommand(genericclioptions.NewConfigFlags(true))
	output, err := captureStdout(t, func() error {
		return cmd.RunE(cmd, []string{"target"})
	})
	require.NoError(t, err)
	require.Contains(t, output, "target")
}
