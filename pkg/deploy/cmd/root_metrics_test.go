package cmd

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeployRootCommand_HasMetricsAddrFlag(t *testing.T) {
	cmd := NewRootCommand()
	flag := cmd.PersistentFlags().Lookup("metrics-addr")
	require.NotNil(t, flag, "expected metrics-addr flag to be registered")
	require.Equal(t, "string", flag.Value.Type(), "metrics-addr flag should be a string")
	require.Empty(t, flag.DefValue, "metrics-addr should default to empty (disabled)")
}

func TestDeployRootCommandRunE_MetricsServerStartFailureIsReturned(t *testing.T) {
	oldMCPServer, oldMetricsAddr := mcpServer, metricsAddr
	oldStart, oldShutdown, oldRunMCPServer := startMetricsServer, shutdownMetricsServer, runMCPServer
	t.Cleanup(func() {
		mcpServer = oldMCPServer
		metricsAddr = oldMetricsAddr
		startMetricsServer = oldStart
		shutdownMetricsServer = oldShutdown
		runMCPServer = oldRunMCPServer
	})

	startMetricsServer = func(addr string) (*http.Server, error) {
		require.Equal(t, ":0", addr)
		return nil, errors.New("bind boom")
	}
	shutdownCalled := false
	shutdownMetricsServer = func(ctx context.Context, srv *http.Server) error {
		shutdownCalled = true
		return nil
	}
	runMCPServer = func() error {
		t.Fatalf("MCP server should not be invoked when metrics startup fails")
		return nil
	}

	cmd := NewRootCommand()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	require.NoError(t, cmd.PersistentFlags().Set("mcp-server", "true"))
	require.NoError(t, cmd.PersistentFlags().Set("metrics-addr", ":0"))

	err := cmd.RunE(cmd, nil)
	require.EqualError(t, err, "bind boom")
	require.False(t, shutdownCalled, "shutdown must not run when start failed")
}

func TestDeployRootCommandRunE_StartsMetricsServerAndShutsDown(t *testing.T) {
	oldMCPServer, oldMetricsAddr := mcpServer, metricsAddr
	oldStart, oldShutdown, oldRunMCPServer := startMetricsServer, shutdownMetricsServer, runMCPServer
	t.Cleanup(func() {
		mcpServer = oldMCPServer
		metricsAddr = oldMetricsAddr
		startMetricsServer = oldStart
		shutdownMetricsServer = oldShutdown
		runMCPServer = oldRunMCPServer
	})

	sentinel := &http.Server{}
	startCalled := false
	startMetricsServer = func(addr string) (*http.Server, error) {
		startCalled = true
		require.Equal(t, "127.0.0.1:0", addr)
		return sentinel, nil
	}
	shutdownCalled := false
	shutdownMetricsServer = func(ctx context.Context, srv *http.Server) error {
		shutdownCalled = true
		require.Same(t, sentinel, srv)
		require.NotNil(t, ctx)
		return nil
	}
	runCalled := false
	runMCPServer = func() error {
		runCalled = true
		return nil
	}

	cmd := NewRootCommand()
	require.NoError(t, cmd.PersistentFlags().Set("mcp-server", "true"))
	require.NoError(t, cmd.PersistentFlags().Set("metrics-addr", "127.0.0.1:0"))
	require.NoError(t, cmd.RunE(cmd, nil))

	require.True(t, startCalled, "expected metrics server start to be invoked")
	require.True(t, runCalled, "expected MCP runner to be invoked")
	require.True(t, shutdownCalled, "expected deferred metrics shutdown to be invoked")
}

func TestDeployRootCommandRunE_NoMetricsAddrSkipsServer(t *testing.T) {
	oldMCPServer, oldMetricsAddr := mcpServer, metricsAddr
	oldStart, oldRunMCPServer := startMetricsServer, runMCPServer
	t.Cleanup(func() {
		mcpServer = oldMCPServer
		metricsAddr = oldMetricsAddr
		startMetricsServer = oldStart
		runMCPServer = oldRunMCPServer
	})

	startMetricsServer = func(addr string) (*http.Server, error) {
		t.Fatalf("metrics server should not be started when metrics-addr is empty")
		return nil, nil
	}
	runCalled := false
	runMCPServer = func() error {
		runCalled = true
		return nil
	}

	cmd := NewRootCommand()
	require.NoError(t, cmd.PersistentFlags().Set("mcp-server", "true"))
	require.NoError(t, cmd.RunE(cmd, nil))
	require.True(t, runCalled, "expected MCP runner to be invoked")
}
