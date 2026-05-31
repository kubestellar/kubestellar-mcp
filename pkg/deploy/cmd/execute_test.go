package cmd

import (
	"bytes"
	"errors"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestDeployRootCommandRunEUsesHelpWhenNotRunningMCP(t *testing.T) {
	oldMCPServer := mcpServer
	t.Cleanup(func() { mcpServer = oldMCPServer })
	mcpServer = false

	cmd := NewRootCommand()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	require.NoError(t, cmd.RunE(cmd, nil))
	require.Contains(t, output.String(), "kubestellar-deploy")
}

func TestDeployRootCommandRunEInvokesMCPServer(t *testing.T) {
	oldMCPServer, oldRunMCPServer := mcpServer, runMCPServer
	t.Cleanup(func() {
		mcpServer = oldMCPServer
		runMCPServer = oldRunMCPServer
	})
	called := false
	runMCPServer = func() error {
		called = true
		return nil
	}

	cmd := NewRootCommand()
	require.NoError(t, cmd.PersistentFlags().Set("mcp-server", "true"))
	require.NoError(t, cmd.RunE(cmd, nil))
	require.True(t, called, "expected MCP server runner to be called")
}

func TestExecuteUsesCommandFactoryAndReportsErrors(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		oldFactory, oldStderr := newRootCommand, stderr
		t.Cleanup(func() {
			newRootCommand = oldFactory
			stderr = oldStderr
		})

		called := false
		newRootCommand = func() *cobra.Command {
			return &cobra.Command{Use: "test", RunE: func(cmd *cobra.Command, args []string) error {
				called = true
				return nil
			}}
		}
		stderr = &bytes.Buffer{}

		require.NoError(t, Execute())
		require.True(t, called, "expected Execute to invoke command")
	})

	t.Run("error", func(t *testing.T) {
		oldFactory, oldStderr := newRootCommand, stderr
		t.Cleanup(func() {
			newRootCommand = oldFactory
			stderr = oldStderr
		})

		var errBuf bytes.Buffer
		newRootCommand = func() *cobra.Command {
			return &cobra.Command{Use: "test", RunE: func(cmd *cobra.Command, args []string) error {
				return errors.New("run failed")
			}}
		}
		stderr = &errBuf

		err := Execute()
		require.EqualError(t, err, "run failed")
		require.Contains(t, errBuf.String(), "run failed")
	})
}
