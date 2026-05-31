package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

type fakeMCPRunner struct {
	runFn func(context.Context) error
}

func (f fakeMCPRunner) Run(ctx context.Context) error {
	if f.runFn != nil {
		return f.runFn(ctx)
	}
	return nil
}

type exitCode int

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()

	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = writer
	defer func() { os.Stdout = oldStdout }()

	runErr := fn()
	require.NoError(t, writer.Close())

	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	return string(data), runErr
}

func TestRootRunShowsHelpWhenNoArgs(t *testing.T) {
	helpCalled := false
	cmd := &cobra.Command{}
	cmd.SetHelpFunc(func(*cobra.Command, []string) {
		helpCalled = true
	})

	rootCmd.Run(cmd, nil)
	require.True(t, helpCalled, "expected help to be shown")
}

func TestRootRunExecutesNaturalLanguageQuery(t *testing.T) {
	oldNewQueryCommand := newQueryCommand
	t.Cleanup(func() { newQueryCommand = oldNewQueryCommand })

	var capturedArgs []string
	newQueryCommand = func(flags *genericclioptions.ConfigFlags) *cobra.Command {
		require.NotNil(t, flags)
		return &cobra.Command{Use: "query", RunE: func(cmd *cobra.Command, args []string) error {
			capturedArgs = append([]string{}, args...)
			return nil
		}}
	}

	rootCmd.Run(rootCmd, []string{"show", "failing", "pods"})
	require.Equal(t, []string{"show", "failing", "pods"}, capturedArgs)
}

func TestRootRunExitsWhenNaturalLanguageQueryFails(t *testing.T) {
	oldNewQueryCommand, oldExitFunc, oldStderr := newQueryCommand, exitFunc, stderr
	t.Cleanup(func() {
		newQueryCommand = oldNewQueryCommand
		exitFunc = oldExitFunc
		stderr = oldStderr
	})

	var errBuf bytes.Buffer
	newQueryCommand = func(flags *genericclioptions.ConfigFlags) *cobra.Command {
		return &cobra.Command{Use: "query", RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("query failed")
		}}
	}
	exitFunc = func(code int) { panic(exitCode(code)) }
	stderr = &errBuf

	defer func() {
		recovered := recover()
		code, ok := recovered.(exitCode)
		require.True(t, ok, "expected exitCode panic, got %#v", recovered)
		require.Equal(t, exitCode(1), code)
		require.Contains(t, errBuf.String(), "query failed")
	}()

	rootCmd.Run(rootCmd, []string{"show", "failing", "pods"})
}

func TestRootRunStartsMCPServer(t *testing.T) {
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
	kubeconfigPath := "custom-kubeconfig"
	configFlags.KubeConfig = &kubeconfigPath
	signalNotify = func(c chan<- os.Signal, sig ...os.Signal) {}

	called := false
	newMCPServer = func(kubeconfig string) mcpServerRunner {
		require.Equal(t, kubeconfigPath, kubeconfig)
		return fakeMCPRunner{runFn: func(ctx context.Context) error {
			called = true
			require.NotNil(t, ctx)
			return nil
		}}
	}

	rootCmd.Run(rootCmd, nil)
	require.True(t, called, "expected MCP runner to be called")
}

func TestRootRunExitsWhenMCPServerFails(t *testing.T) {
	oldMCPServer, oldConfigFlags := mcpServer, configFlags
	oldNewMCPServer, oldSignalNotify := newMCPServer, signalNotify
	oldExitFunc, oldStderr := exitFunc, stderr
	t.Cleanup(func() {
		mcpServer = oldMCPServer
		configFlags = oldConfigFlags
		newMCPServer = oldNewMCPServer
		signalNotify = oldSignalNotify
		exitFunc = oldExitFunc
		stderr = oldStderr
	})

	mcpServer = true
	configFlags = genericclioptions.NewConfigFlags(true)
	signalNotify = func(c chan<- os.Signal, sig ...os.Signal) {}
	exitFunc = func(code int) { panic(exitCode(code)) }
	var errBuf bytes.Buffer
	stderr = &errBuf
	newMCPServer = func(string) mcpServerRunner {
		return fakeMCPRunner{runFn: func(ctx context.Context) error {
			return errors.New("server boom")
		}}
	}

	defer func() {
		recovered := recover()
		code, ok := recovered.(exitCode)
		require.True(t, ok, "expected exitCode panic, got %#v", recovered)
		require.Equal(t, exitCode(1), code)
		require.Contains(t, errBuf.String(), "server boom")
	}()

	rootCmd.Run(rootCmd, nil)
}

func TestExecuteRunsVersionCommand(t *testing.T) {
	oldArgs := os.Args
	rootCmd.SetArgs([]string{"version"})
	defer func() {
		os.Args = oldArgs
		rootCmd.SetArgs(nil)
	}()

	output, err := captureStdout(t, Execute)
	require.NoError(t, err)
	require.Contains(t, output, "kubestellar-ops version")
}
