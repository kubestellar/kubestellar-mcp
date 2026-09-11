package cmd

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

// TestRootCmdArgsAllowsAnyArguments hits the inline Args callback at
// pkg/cmd/root.go:42 (`return nil`), which was previously uncovered
// because the existing suite only ever calls rootCmd.Run directly and
// never rootCmd.Execute()/args validation.
func TestRootCmdArgsAllowsAnyArguments(t *testing.T) {
	require.NotNil(t, rootCmd.Args)
	// Any positional args must be accepted.
	require.NoError(t, rootCmd.Args(rootCmd, nil))
	require.NoError(t, rootCmd.Args(rootCmd, []string{"anything", "goes"}))
}

// The following three tests exercise the `return` statement that
// follows each `exitFunc(1)` call in rootCmd.Run. When exitFunc is
// stubbed to os.Exit (production) those returns are dead code, and
// when tests stub exitFunc to panic they are unreachable too. This
// suite uses a *non-panicking* exitFunc so execution falls through to
// the `return` — which is what protects the surrounding logic from
// double-executing in any exitFunc that doesn't terminate the process
// (e.g. a test harness that records the code without exiting).

func TestRootRunReturnsAfterMetricsServerStartFailure(t *testing.T) {
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

	exitCalls := 0
	exitFunc = func(code int) { exitCalls++; require.Equal(t, 1, code) }

	startMetricsServer = func(addr string) (*http.Server, error) {
		return nil, errors.New("bind boom")
	}
	shutdownCalled := false
	shutdownMetricsServer = func(ctx context.Context, srv *http.Server) error {
		shutdownCalled = true
		return nil
	}
	mcpRunCalled := false
	newMCPServer = func(string) mcpServerRunner {
		return fakeMCPRunner{runFn: func(ctx context.Context) error {
			mcpRunCalled = true
			return nil
		}}
	}

	rootCmd.Run(rootCmd, nil)

	require.Equal(t, 1, exitCalls, "exitFunc should have been called exactly once")
	require.False(t, mcpRunCalled, "the `return` after exitFunc must prevent MCP runner from executing")
	require.False(t, shutdownCalled, "the `return` must skip the deferred shutdown as well (defer wasn't set up before the return)")
	require.Contains(t, errBuf.String(), "metrics server error")
}

func TestRootRunReturnsAfterNaturalLanguageQueryFailure(t *testing.T) {
	oldNewQueryCommand, oldExitFunc, oldStderr := newQueryCommand, exitFunc, stderr
	t.Cleanup(func() {
		newQueryCommand = oldNewQueryCommand
		exitFunc = oldExitFunc
		stderr = oldStderr
	})

	var errBuf bytes.Buffer
	stderr = &errBuf

	exitCalls := 0
	exitFunc = func(code int) { exitCalls++; require.Equal(t, 1, code) }

	newQueryCommand = func(flags *genericclioptions.ConfigFlags) *cobra.Command {
		return &cobra.Command{
			Use:  "query",
			RunE: func(cmd *cobra.Command, args []string) error { return errors.New("query boom") },
		}
	}

	// Passing >1 args that look like natural language triggers the
	// isNaturalLanguageQuery branch even when the MCP server flag is
	// not set — restore it defensively.
	oldMCPServer := mcpServer
	t.Cleanup(func() { mcpServer = oldMCPServer })
	mcpServer = false

	rootCmd.Run(rootCmd, []string{"show", "failing", "pods"})

	require.Equal(t, 1, exitCalls, "exitFunc must fire on query-command error")
	require.Contains(t, errBuf.String(), "query boom")
	// If the `return` after exitFunc were missing, execution would
	// fall through to `cmd.Help()` and no error would have been on
	// stderr — the fact that we see "query boom" and only exit=1
	// confirms the return path was taken.
}

