package cmd

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

// TestRootRunMCPServerCancelsContextOnShutdownSignal exercises the shutdown
// goroutine started in the MCP-server branch of rootCmd.Run: it delivers a
// real signal through the channel signalNotify hands out and asserts the
// context passed to the MCP runner is canceled as a result, matching the
// graceful-shutdown contract of --mcp-server.
func TestRootRunMCPServerCancelsContextOnShutdownSignal(t *testing.T) {
	oldMCPServer, oldConfigFlags := mcpServer, configFlags
	oldNewMCPServer, oldSignalNotify := newMCPServer, signalNotify
	t.Cleanup(func() {
		mcpServer = oldMCPServer
		configFlags = oldConfigFlags
		newMCPServer = oldNewMCPServer
		signalNotify = oldSignalNotify
	})

	mcpServer = true
	configFlags = genericclioptions.NewConfigFlags(true)

	// Deliver the shutdown signal synchronously through the buffered
	// channel so the goroutine that awaits it can run and call cancel().
	signalNotify = func(c chan<- os.Signal, sig ...os.Signal) {
		c <- syscall.SIGINT
	}

	ctxCanceled := make(chan struct{})
	newMCPServer = func(string) mcpServerRunner {
		return fakeMCPRunner{runFn: func(ctx context.Context) error {
			<-ctx.Done()
			close(ctxCanceled)
			return nil
		}}
	}

	rootCmd.Run(rootCmd, nil)

	select {
	case <-ctxCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("expected shutdown signal to cancel the MCP server context")
	}
}

// TestNewMCPServerDefaultConstructsRealServer exercises the default
// newMCPServer var (never overridden by any test cleanup) to guard the
// production wiring between rootCmd and pkg/mcp/server.NewServer.
func TestNewMCPServerDefaultConstructsRealServer(t *testing.T) {
	srv := newMCPServer("some-kubeconfig-path")
	require.NotNil(t, srv, "expected default newMCPServer to construct a runner")
}
