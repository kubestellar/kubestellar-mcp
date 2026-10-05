//go:build integration

package integration

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/kubestellar/kubestellar-mcp/pkg/cmd/ai"
	"github.com/kubestellar/kubestellar-mcp/pkg/cmd/clusters"
	"github.com/kubestellar/kubestellar-mcp/pkg/cmd/upgrade"
)

// captureStdout runs fn with os.Stdout redirected to a pipe, asserts fn
// returned no error, and returns everything written to it. pkg/cmd/clusters's
// list/health commands print straight to os.Stdout (not cmd.OutOrStdout()),
// so redirecting the process-global os.Stdout - the same technique
// pkg/cmd/clusters/run_test.go uses for its in-package unit tests - is the
// only way to observe their output here too.
func captureStdout(t *testing.T, fn func() error) string {
	t.Helper()

	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = writer
	defer func() { os.Stdout = oldStdout }()

	runErr := fn()
	require.NoError(t, writer.Close())

	var buf bytes.Buffer
	_, copyErr := io.Copy(&buf, reader)
	require.NoError(t, copyErr)
	require.NoError(t, reader.Close())

	require.NoError(t, runErr)
	return buf.String()
}

// newIntegrationConfigFlags builds the same genericclioptions.ConfigFlags
// that pkg/cmd/root.go wires into every subcommand, pointed at the shared
// envtest kubeconfig (writeIntegrationKubeconfig, cluster_test.go) rather
// than a real user's kubeconfig. This is the one piece these tests fake:
// everything downstream of it - clientcmd loading rules, discovery,
// dynamic/typed client construction, and the actual apiserver round-trips -
// is the real code path a user hits running the kubestellar-ops binary.
func newIntegrationConfigFlags(t *testing.T) *genericclioptions.ConfigFlags {
	t.Helper()

	kubeconfigPath := writeIntegrationKubeconfig(t)
	configFlags := genericclioptions.NewConfigFlags(true)
	configFlags.KubeConfig = &kubeconfigPath
	return configFlags
}

// TestClustersCommandsEndToEnd drives pkg/cmd/clusters's "list" and "health"
// cobra commands (exactly as root.go wires them via
// clusters.NewClustersCommand) through cmd.Execute(), against a real
// kube-apiserver (envtest), rather than through the package's own fake
// clusterDiscoverer (see pkg/cmd/clusters/run_test.go). This exercises the
// real wiring the unit tests never touch: cobra flag/arg parsing, the
// package-level newDiscoverer factory constructing a real
// pkg/cluster.Discoverer, clientcmd loading the kubeconfig from disk, and
// live Discovery().ServerVersion()/Nodes().List() calls - the same
// envtest-backed client path TestClusterTools (cluster_test.go) exercises
// for the MCP tool surface, now entered through the actual CLI command tree.
func TestClustersCommandsEndToEnd(t *testing.T) {
	configFlags := newIntegrationConfigFlags(t)

	t.Run("list reports the envtest context", func(t *testing.T) {
		cmd := clusters.NewClustersCommand(configFlags)
		out := &captureWriter{}
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs([]string{"list"})

		stdout := captureStdout(t, func() error {
			return cmd.ExecuteContext(context.Background())
		})

		require.Contains(t, stdout, integrationClusterName)
		require.Contains(t, stdout, "kubeconfig")
	})

	t.Run("health reports the current context as healthy", func(t *testing.T) {
		cmd := clusters.NewClustersCommand(configFlags)
		out := &captureWriter{}
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs([]string{"health"})

		stdout := captureStdout(t, func() error {
			return cmd.ExecuteContext(context.Background())
		})

		require.Contains(t, stdout, integrationClusterName)
		require.Contains(t, stdout, "Healthy")
		// envtest runs no kubelet, so the node-readiness count is always 0/0.
		require.Contains(t, stdout, "0/0")
	})
}

// TestWatchUpgradeCommandEndToEnd drives pkg/cmd/upgrade's "watch-upgrade"
// cobra command (upgrade.NewWatchCommand, the same constructor root.go
// registers) through cmd.Execute() against a real kube-apiserver (envtest).
// Unlike the package's unit tests (watch_command_test.go et al.), which
// inject a fake dynamic.Interface, this uses clientcmd's real loading rules
// to build the client from an on-disk kubeconfig and issues a real
// dynamic Get for the OpenShift ClusterVersion CRD. envtest has no such CRD
// installed, so ensureOpenShiftCluster's real apiserver round-trip returns a
// real not-found error before the watch loop ever starts - the same
// deterministic "vanilla Kubernetes" shape TestUpgradesKubernetesPath
// (upgrades_test.go) asserts on for the MCP tool surface's upgrade probes.
func TestWatchUpgradeCommandEndToEnd(t *testing.T) {
	configFlags := newIntegrationConfigFlags(t)

	cmd := upgrade.NewWatchCommand(configFlags)
	out := &captureWriter{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{})

	err := cmd.ExecuteContext(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "not an OpenShift cluster")
}

// TestQueryCommandMissingAPIKeyEndToEnd drives pkg/cmd/ai's "query" cobra
// command (ai.NewQueryCommand, the same constructor root.go registers)
// through cmd.Execute() with no fakes at all - unlike the package's unit
// tests (query_test.go), which stub both the Claude client and the cluster
// discoverer via stubQueryDependencies. The Claude API is an external SaaS
// boundary with no local override hook (pkg/ai/claude.NewClient always
// reads ANTHROPIC_API_KEY from the real process environment and has no
// base-URL override reachable from the CLI), so hitting it for real is out
// of scope for an envtest-backed suite, mirroring how pkg/deploy/mcp/helm is
// documented out of scope in README.md for the same reason: no local,
// deterministic substitute for an external dependency. What this test does
// exercise end-to-end is the real cobra wiring a user actually hits running
// `kubestellar-ops ai query "..."` without ANTHROPIC_API_KEY configured -
// Args validation, RunE dispatch, and the real claude.NewClient error path -
// none of which the fully-stubbed unit tests touch.
func TestQueryCommandMissingAPIKeyEndToEnd(t *testing.T) {
	oldKey, hadKey := os.LookupEnv("ANTHROPIC_API_KEY")
	require.NoError(t, os.Unsetenv("ANTHROPIC_API_KEY"))
	t.Cleanup(func() {
		if hadKey {
			_ = os.Setenv("ANTHROPIC_API_KEY", oldKey)
		}
	})

	configFlags := newIntegrationConfigFlags(t)

	cmd := ai.NewQueryCommand(configFlags)
	out := &captureWriter{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"what", "is", "wrong", "with", "my", "cluster"})

	err := cmd.ExecuteContext(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "ANTHROPIC_API_KEY")
}

// captureWriter is a minimal io.Writer sink for cobra's SetOut/SetErr, used
// where the assertions only care about the returned error, not anything
// cobra itself might print (usage text on error, etc.).
type captureWriter struct{}

func (captureWriter) Write(p []byte) (int, error) { return len(p), nil }
