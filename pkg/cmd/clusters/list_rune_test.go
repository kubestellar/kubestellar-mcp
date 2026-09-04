package clusters

import (
	"testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/cluster"
	"github.com/stretchr/testify/require"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

// TestListCommandRunEInvokesRun exercises the RunE closure of the cobra
// command so the `return o.run()` line in newListCommand is covered. This
// mirrors TestHealthCommandRunEUsesFirstArg for the list subcommand.
func TestListCommandRunEInvokesRun(t *testing.T) {
	oldFactory := newDiscoverer
	t.Cleanup(func() { newDiscoverer = oldFactory })
	newDiscoverer = func(string) clusterDiscoverer {
		return fakeDiscoverer{discoverClustersFn: func(source string) ([]cluster.ClusterInfo, error) {
			require.Equal(t, "all", source)
			return []cluster.ClusterInfo{{
				Name:   "target",
				Source: "kubeconfig",
				Server: "https://api.example:6443",
				Status: "Healthy",
			}}, nil
		}}
	}

	cmd := newListCommand(genericclioptions.NewConfigFlags(true))
	output, err := captureStdout(t, func() error {
		return cmd.RunE(cmd, []string{})
	})
	require.NoError(t, err)
	require.Contains(t, output, "target")
	require.Contains(t, output, "kubeconfig")
}
