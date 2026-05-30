package ai

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/kubestellar/kubestellar-mcp/pkg/ai/claude"
	"github.com/kubestellar/kubestellar-mcp/pkg/cluster"
)

type fakeClaudeQueryClient struct {
	response     string
	err          error
	systemPrompt string
	userQuery    string
}

func (f *fakeClaudeQueryClient) Query(_ context.Context, systemPrompt, userQuery string) (string, error) {
	f.systemPrompt = systemPrompt
	f.userQuery = userQuery
	return f.response, f.err
}

type fakeClusterDiscoverer struct {
	clusters         []cluster.ClusterInfo
	discoverErr      error
	health           *cluster.HealthInfo
	healthErr        error
	discoveredSource string
	checkedCluster   cluster.ClusterInfo
	checkHealthCalls int
}

func (f *fakeClusterDiscoverer) DiscoverClusters(source string) ([]cluster.ClusterInfo, error) {
	f.discoveredSource = source
	return f.clusters, f.discoverErr
}

func (f *fakeClusterDiscoverer) CheckHealth(clusterInfo cluster.ClusterInfo) (*cluster.HealthInfo, error) {
	f.checkHealthCalls++
	f.checkedCluster = clusterInfo
	return f.health, f.healthErr
}

func TestNewQueryCommand_MetadataDefaultsAndArgs(t *testing.T) {
	configFlags := genericclioptions.NewConfigFlags(true)
	cmd := NewQueryCommand(configFlags)

	require.Equal(t, "query <natural language query>", cmd.Use)
	require.Equal(t, claude.DefaultModel, cmd.Flag("model").DefValue)
	require.Equal(t, "false", cmd.Flag("include-status").DefValue)
	require.Error(t, cmd.Args(cmd, nil))
	require.NoError(t, cmd.Args(cmd, []string{"show pods"}))
}

func TestQueryCommand_JoinsArgsAndBuildsClusterContext(t *testing.T) {
	client := &fakeClaudeQueryClient{response: "pods listed"}
	discoverer := &fakeClusterDiscoverer{clusters: []cluster.ClusterInfo{
		{Name: "alpha", Current: true},
		{Name: "beta"},
	}}
	_, restore := stubQueryDependencies(t, client, discoverer)
	defer restore()

	configFlags := genericclioptions.NewConfigFlags(true)
	namespace := "demo"
	kubeconfig := "/home/dev/kubestellar-mcp-81/test-kubeconfig"
	configFlags.Namespace = &namespace
	configFlags.KubeConfig = &kubeconfig

	cmd := NewQueryCommand(configFlags)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"show", "pods"})

	output := captureStdout(t, func() {
		require.NoError(t, cmd.ExecuteContext(context.Background()))
	})

	require.Equal(t, "all", discoverer.discoveredSource)
	require.Equal(t, "show pods", client.userQuery)
	require.Contains(t, client.systemPrompt, "- Current cluster: alpha")
	require.Contains(t, client.systemPrompt, "- Current namespace: demo")
	require.Contains(t, client.systemPrompt, "- Available clusters: alpha, beta")
	require.Zero(t, discoverer.checkHealthCalls)
	require.Contains(t, output, "Thinking...")
	require.Contains(t, output, "pods listed")
}

func TestQueryCommand_IncludeStatusAddsHealthContext(t *testing.T) {
	client := &fakeClaudeQueryClient{response: "cluster looks healthy"}
	discoverer := &fakeClusterDiscoverer{
		clusters: []cluster.ClusterInfo{{Name: "alpha", Current: true}},
		health: &cluster.HealthInfo{
			Status:          "Healthy",
			NodesReady:      "3/3",
			APIServerStatus: "Healthy",
		},
	}
	capturedModel, restore := stubQueryDependencies(t, client, discoverer)
	defer restore()

	configFlags := genericclioptions.NewConfigFlags(true)
	cmd := NewQueryCommand(configFlags)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--model", "claude-test", "--include-status", "what", "is", "the", "cluster", "status"})

	captureStdout(t, func() {
		require.NoError(t, cmd.ExecuteContext(context.Background()))
	})

	require.Equal(t, "claude-test", *capturedModel)
	require.Equal(t, 1, discoverer.checkHealthCalls)
	require.Equal(t, "alpha", discoverer.checkedCluster.Name)
	require.Contains(t, client.userQuery, "Additional context from the cluster")
	require.Contains(t, client.userQuery, "Current cluster: alpha")
	require.Contains(t, client.userQuery, "Status: Healthy")
	require.Contains(t, client.userQuery, "Nodes: 3/3")
	require.Contains(t, client.userQuery, "API Server: Healthy")
}

func TestQueryCommand_DiscoveryFailureWarnsAndContinues(t *testing.T) {
	client := &fakeClaudeQueryClient{response: "fallback answer"}
	discoverer := &fakeClusterDiscoverer{discoverErr: errors.New("discovery failed")}
	_, restore := stubQueryDependencies(t, client, discoverer)
	defer restore()

	configFlags := genericclioptions.NewConfigFlags(true)
	cmd := NewQueryCommand(configFlags)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"why", "is", "my", "deployment", "pending"})

	output := captureStdout(t, func() {
		require.NoError(t, cmd.ExecuteContext(context.Background()))
	})

	require.Equal(t, "why is my deployment pending", client.userQuery)
	require.Contains(t, output, "Warning: failed to discover clusters: discovery failed")
	require.Contains(t, output, "fallback answer")
}

func TestQueryCommand_ReturnsClaudeClientCreationError(t *testing.T) {
	clientErr := errors.New("missing API key")
	originalClientFactory := newClaudeQueryClient
	originalDiscovererFactory := newClusterDiscoverer
	t.Cleanup(func() {
		newClaudeQueryClient = originalClientFactory
		newClusterDiscoverer = originalDiscovererFactory
	})

	newClaudeQueryClient = func(string) (claudeQueryClient, error) {
		return nil, clientErr
	}
	newClusterDiscoverer = func(string) clusterDiscoverer {
		return &fakeClusterDiscoverer{}
	}

	configFlags := genericclioptions.NewConfigFlags(true)
	cmd := NewQueryCommand(configFlags)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"show", "pods"})

	err := cmd.ExecuteContext(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to create Claude client: missing API key")
	require.Contains(t, err.Error(), "Make sure ANTHROPIC_API_KEY environment variable is set")
}

func stubQueryDependencies(t *testing.T, client *fakeClaudeQueryClient, discoverer *fakeClusterDiscoverer) (*string, func()) {
	t.Helper()

	capturedModel := new(string)
	originalClientFactory := newClaudeQueryClient
	originalDiscovererFactory := newClusterDiscoverer

	newClaudeQueryClient = func(model string) (claudeQueryClient, error) {
		*capturedModel = model
		return client, nil
	}
	newClusterDiscoverer = func(string) clusterDiscoverer {
		return discoverer
	}

	return capturedModel, func() {
		newClaudeQueryClient = originalClientFactory
		newClusterDiscoverer = originalDiscovererFactory
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	originalStdout := os.Stdout
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = writer

	fn()

	require.NoError(t, writer.Close())
	os.Stdout = originalStdout

	var buf bytes.Buffer
	_, err = io.Copy(&buf, reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())

	return strings.ReplaceAll(buf.String(), "\r\n", "\n")
}
