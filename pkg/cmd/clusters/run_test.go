package clusters

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/cluster"
	"github.com/stretchr/testify/require"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

type fakeDiscoverer struct {
	discoverClustersFn func(string) ([]cluster.ClusterInfo, error)
	checkHealthFn      func(cluster.ClusterInfo) (*cluster.HealthInfo, error)
}

func (f fakeDiscoverer) DiscoverClusters(source string) ([]cluster.ClusterInfo, error) {
	if f.discoverClustersFn != nil {
		return f.discoverClustersFn(source)
	}
	return nil, nil
}

func (f fakeDiscoverer) CheckHealth(info cluster.ClusterInfo) (*cluster.HealthInfo, error) {
	if f.checkHealthFn != nil {
		return f.checkHealthFn(info)
	}
	return nil, nil
}

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()

	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = writer
	defer func() {
		os.Stdout = oldStdout
	}()

	runErr := fn()
	require.NoError(t, writer.Close())

	var buf bytes.Buffer
	_, copyErr := io.Copy(&buf, reader)
	require.NoError(t, copyErr)
	require.NoError(t, reader.Close())

	return buf.String(), runErr
}

func TestListRunReturnsDiscoveryError(t *testing.T) {
	oldFactory := newDiscoverer
	t.Cleanup(func() { newDiscoverer = oldFactory })
	newDiscoverer = func(kubeconfig string) clusterDiscoverer {
		return fakeDiscoverer{discoverClustersFn: func(source string) ([]cluster.ClusterInfo, error) {
			require.Equal(t, "all", source)
			return nil, errors.New("discover failed")
		}}
	}

	o := &listOptions{configFlags: genericclioptions.NewConfigFlags(true), source: "all"}
	_, err := captureStdout(t, o.run)
	require.EqualError(t, err, "failed to discover clusters: discover failed")
}

func TestListRunPrintsNoClustersMessage(t *testing.T) {
	oldFactory := newDiscoverer
	t.Cleanup(func() { newDiscoverer = oldFactory })
	newDiscoverer = func(string) clusterDiscoverer {
		return fakeDiscoverer{discoverClustersFn: func(source string) ([]cluster.ClusterInfo, error) {
			return []cluster.ClusterInfo{}, nil
		}}
	}

	o := &listOptions{configFlags: genericclioptions.NewConfigFlags(true), source: "all"}
	output, err := captureStdout(t, o.run)
	require.NoError(t, err)
	require.Contains(t, output, "No clusters found")
}

func TestListRunPrintsClusterTable(t *testing.T) {
	oldFactory := newDiscoverer
	t.Cleanup(func() { newDiscoverer = oldFactory })
	newDiscoverer = func(string) clusterDiscoverer {
		return fakeDiscoverer{discoverClustersFn: func(source string) ([]cluster.ClusterInfo, error) {
			require.Equal(t, "all", source)
			return []cluster.ClusterInfo{{
				Name:    "prod-east",
				Source:  "kubeconfig",
				Server:  "https://very-long-control-plane.example.internal:6443",
				Current: true,
				Status:  "Healthy",
			}}, nil
		}}
	}

	o := &listOptions{configFlags: genericclioptions.NewConfigFlags(true), source: "all"}
	output, err := captureStdout(t, o.run)
	require.NoError(t, err)
	require.Contains(t, output, "CURRENT")
	require.Contains(t, output, "prod-east")
	require.Contains(t, output, "kubeconfig")
	require.Contains(t, output, "Healthy")
	require.Contains(t, output, "*")
	require.Contains(t, output, truncateString("https://very-long-control-plane.example.internal:6443", 50))
}

func TestHealthRunReturnsErrorsForDiscoveryAndMissingCluster(t *testing.T) {
	t.Run("discover all clusters failure", func(t *testing.T) {
		oldFactory := newDiscoverer
		t.Cleanup(func() { newDiscoverer = oldFactory })
		newDiscoverer = func(string) clusterDiscoverer {
			return fakeDiscoverer{discoverClustersFn: func(source string) ([]cluster.ClusterInfo, error) {
				require.Equal(t, "all", source)
				return nil, errors.New("boom")
			}}
		}

		o := &healthOptions{configFlags: genericclioptions.NewConfigFlags(true), allClusters: true}
		_, err := captureStdout(t, o.run)
		require.EqualError(t, err, "failed to discover clusters: boom")
	})

	t.Run("named cluster not found", func(t *testing.T) {
		oldFactory := newDiscoverer
		t.Cleanup(func() { newDiscoverer = oldFactory })
		newDiscoverer = func(string) clusterDiscoverer {
			return fakeDiscoverer{discoverClustersFn: func(source string) ([]cluster.ClusterInfo, error) {
				require.Equal(t, "all", source)
				return []cluster.ClusterInfo{{Name: "dev"}}, nil
			}}
		}

		o := &healthOptions{configFlags: genericclioptions.NewConfigFlags(true), clusterName: "prod"}
		_, err := captureStdout(t, o.run)
		require.EqualError(t, err, "cluster \"prod\" not found")
	})
}

func TestHealthRunPrintsNoClustersWhenNoCurrentContext(t *testing.T) {
	oldFactory := newDiscoverer
	t.Cleanup(func() { newDiscoverer = oldFactory })
	newDiscoverer = func(string) clusterDiscoverer {
		return fakeDiscoverer{discoverClustersFn: func(source string) ([]cluster.ClusterInfo, error) {
			require.Equal(t, "kubeconfig", source)
			return []cluster.ClusterInfo{{Name: "dev", Current: false}}, nil
		}}
	}

	o := &healthOptions{configFlags: genericclioptions.NewConfigFlags(true)}
	output, err := captureStdout(t, o.run)
	require.NoError(t, err)
	require.Contains(t, output, "No clusters to check")
}

func TestHealthRunPrintsHealthTable(t *testing.T) {
	oldFactory := newDiscoverer
	t.Cleanup(func() { newDiscoverer = oldFactory })
	newDiscoverer = func(string) clusterDiscoverer {
		return fakeDiscoverer{
			discoverClustersFn: func(source string) ([]cluster.ClusterInfo, error) {
				require.Equal(t, "all", source)
				return []cluster.ClusterInfo{{Name: "alpha"}, {Name: "beta"}}, nil
			},
			checkHealthFn: func(info cluster.ClusterInfo) (*cluster.HealthInfo, error) {
				if info.Name == "alpha" {
					return &cluster.HealthInfo{Status: "Healthy", NodesReady: "3/3", APIServerStatus: "Healthy", Message: "All systems operational"}, nil
				}
				return nil, errors.New("api unreachable")
			},
		}
	}

	o := &healthOptions{configFlags: genericclioptions.NewConfigFlags(true), allClusters: true}
	output, err := captureStdout(t, o.run)
	require.NoError(t, err)
	require.Contains(t, output, "CLUSTER")
	require.Contains(t, output, "alpha")
	require.Contains(t, output, "Healthy")
	require.Contains(t, output, "3/3")
	require.Contains(t, output, "beta")
	require.Contains(t, output, "Error")
	require.True(t, strings.Contains(output, "api unreachable"))
}
