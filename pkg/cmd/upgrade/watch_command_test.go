package upgrade

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func TestNewWatchCommand(t *testing.T) {
	configFlags := genericclioptions.NewConfigFlags(true)

	cmd := NewWatchCommand(configFlags)

	require.NotNil(t, cmd)
	assert.IsType(t, &cobra.Command{}, cmd)
	assert.Equal(t, "watch-upgrade", cmd.Use)
	assert.Contains(t, cmd.Short, "Watch OpenShift cluster upgrade progress")
	assert.NotNil(t, cmd.RunE)

	intervalFlag := cmd.Flags().Lookup("interval")
	require.NotNil(t, intervalFlag)
	assert.Equal(t, "3s", intervalFlag.DefValue)
	assert.Equal(t, (3 * time.Second).String(), intervalFlag.Value.String())
}

func TestOpenShiftUpgradeGVRs(t *testing.T) {
	assert.Equal(t, schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1",
		Resource: "clusterversions",
	}, clusterVersionGVR)

	assert.Equal(t, schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1",
		Resource: "clusteroperators",
	}, clusterOperatorGVR)

	assert.Equal(t, schema.GroupVersionResource{
		Group:    "machineconfiguration.openshift.io",
		Version:  "v1",
		Resource: "machineconfigpools",
	}, machineConfigPoolGVR)
}

func TestEnsureOpenShiftClusterFailsGracefully(t *testing.T) {
	dynClient := newFakeDynamicClient()

	err := ensureOpenShiftCluster(context.Background(), dynClient)

	require.Error(t, err)
	assert.ErrorContains(t, err, "not an OpenShift cluster or ClusterVersion not accessible")
}

func TestEnsureOpenShiftCluster_Success(t *testing.T) {
	cv := newClusterVersion("4.14.0", "Cluster version is 4.14.0")
	dynClient := newFakeDynamicClient(cv)
	err := ensureOpenShiftCluster(context.Background(), dynClient)
	require.NoError(t, err)
}

func TestWatchUpgrade_CommandWiring(t *testing.T) {
	configFlags := genericclioptions.NewConfigFlags(true)
	cmd := NewWatchCommand(configFlags)

	// Verify the command uses RunE (error-returning variant)
	assert.NotNil(t, cmd.RunE)
	assert.Nil(t, cmd.Run)

	// Verify interval flag can be set
	err := cmd.Flags().Set("interval", "5s")
	require.NoError(t, err)
	assert.Equal(t, "5s", cmd.Flags().Lookup("interval").Value.String())
}

func TestWatchUpgrade_KubeconfigLoadError(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist.yaml")
	explicit := missing
	ctxName := ""
	configFlags := &genericclioptions.ConfigFlags{
		KubeConfig: &explicit,
		Context:    &ctxName,
	}

	t.Setenv("KUBECONFIG", missing)
	t.Setenv("HOME", dir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := watchUpgrade(ctx, configFlags, 10*time.Millisecond)

	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to load kubeconfig")
}

func TestWatchUpgrade_NotOpenShiftCluster(t *testing.T) {
	dir := t.TempDir()
	kc := writeMinimalKubeconfig(t, dir)
	explicit := kc
	ctxName := "bogus"
	configFlags := &genericclioptions.ConfigFlags{
		KubeConfig: &explicit,
		Context:    &ctxName,
	}

	t.Setenv("KUBECONFIG", kc)
	t.Setenv("HOME", dir)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := watchUpgrade(ctx, configFlags, 10*time.Millisecond)

	require.Error(t, err)
	assert.ErrorContains(t, err, "not an OpenShift cluster")
}

func TestWatchUpgrade_CanceledContext_ClientConfigError(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope.yaml")
	explicit := missing
	ctxName := ""
	configFlags := &genericclioptions.ConfigFlags{
		KubeConfig: &explicit,
		Context:    &ctxName,
	}

	t.Setenv("KUBECONFIG", missing)
	t.Setenv("HOME", dir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := watchUpgrade(ctx, configFlags, 10*time.Millisecond)

	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to load kubeconfig")
}

func writeMinimalKubeconfig(t *testing.T, dir string) string {
	t.Helper()

	path := filepath.Join(dir, "kubeconfig")
	content := `apiVersion: v1
kind: Config
clusters:
- name: bogus
  cluster:
    server: http://127.0.0.1:1
contexts:
- name: bogus
  context:
    cluster: bogus
    user: bogus
current-context: bogus
users:
- name: bogus
  user: {}
`

	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}
