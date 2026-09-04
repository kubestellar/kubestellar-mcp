package cmd

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func TestRootRunExitsWhenMetricsServerStartFails(t *testing.T) {
	oldMCPServer, oldConfigFlags := mcpServer, configFlags
	oldNewMCPServer, oldSignalNotify := newMCPServer, signalNotify
	oldMetricsAddr := metricsAddr
	oldStart, oldShutdown := startMetricsServer, shutdownMetricsServer
	oldExitFunc, oldStderr := exitFunc, stderr
	t.Cleanup(func() {
		mcpServer = oldMCPServer
		configFlags = oldConfigFlags
		newMCPServer = oldNewMCPServer
		signalNotify = oldSignalNotify
		metricsAddr = oldMetricsAddr
		startMetricsServer = oldStart
		shutdownMetricsServer = oldShutdown
		exitFunc = oldExitFunc
		stderr = oldStderr
	})

	mcpServer = true
	configFlags = genericclioptions.NewConfigFlags(true)
	signalNotify = func(c chan<- os.Signal, sig ...os.Signal) {}
	metricsAddr = ":0"
	var errBuf bytes.Buffer
	stderr = &errBuf
	exitFunc = func(code int) { panic(exitCode(code)) }

	startMetricsServer = func(addr string) (*http.Server, error) {
		require.Equal(t, ":0", addr)
		return nil, errors.New("bind boom")
	}
	shutdownCalled := false
	shutdownMetricsServer = func(ctx context.Context, srv *http.Server) error {
		shutdownCalled = true
		return nil
	}
	newMCPServer = func(string) mcpServerRunner {
		return fakeMCPRunner{runFn: func(ctx context.Context) error {
			t.Fatalf("MCP runner should not be invoked when metrics startup fails")
			return nil
		}}
	}

	defer func() {
		recovered := recover()
		code, ok := recovered.(exitCode)
		require.True(t, ok, "expected exitCode panic, got %#v", recovered)
		require.Equal(t, exitCode(1), code)
		require.Contains(t, errBuf.String(), "metrics server error")
		require.Contains(t, errBuf.String(), "bind boom")
		require.False(t, shutdownCalled, "shutdown must not run when start failed")
	}()

	rootCmd.Run(rootCmd, nil)
}

func TestRootRunStartsMetricsServerAndShutsDown(t *testing.T) {
	oldMCPServer, oldConfigFlags := mcpServer, configFlags
	oldNewMCPServer, oldSignalNotify := newMCPServer, signalNotify
	oldMetricsAddr := metricsAddr
	oldStart, oldShutdown := startMetricsServer, shutdownMetricsServer
	t.Cleanup(func() {
		mcpServer = oldMCPServer
		configFlags = oldConfigFlags
		newMCPServer = oldNewMCPServer
		signalNotify = oldSignalNotify
		metricsAddr = oldMetricsAddr
		startMetricsServer = oldStart
		shutdownMetricsServer = oldShutdown
	})

	mcpServer = true
	configFlags = genericclioptions.NewConfigFlags(true)
	signalNotify = func(c chan<- os.Signal, sig ...os.Signal) {}
	metricsAddr = "127.0.0.1:0"

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
	newMCPServer = func(string) mcpServerRunner {
		return fakeMCPRunner{runFn: func(ctx context.Context) error {
			runCalled = true
			return nil
		}}
	}

	rootCmd.Run(rootCmd, nil)

	require.True(t, startCalled, "expected metrics server start to be invoked")
	require.True(t, runCalled, "expected MCP runner to be invoked")
	require.True(t, shutdownCalled, "expected deferred metrics shutdown to be invoked")
}
