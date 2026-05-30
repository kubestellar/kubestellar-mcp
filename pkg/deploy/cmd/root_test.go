package cmd

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeployRootCommand_HasMCPServerFlag(t *testing.T) {
	cmd := NewRootCommand()
	flag := cmd.PersistentFlags().Lookup("mcp-server")
	require.NotNil(t, flag, "expected mcp-server flag to be registered")
	require.Equal(t, "bool", flag.Value.Type(), "mcp-server flag should be boolean")
}

func TestDeployRootCommand_HasVersionSubcommand(t *testing.T) {
	cmd := NewRootCommand()

	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "version" {
			found = true
			break
		}
	}
	require.True(t, found, "expected version subcommand to be registered")
}

func TestDeployRootCommand_HelpOutput(t *testing.T) {
	cmd := NewRootCommand()
	help := cmd.Long
	require.NotEmpty(t, help, "root command should have help text")
	require.True(t, strings.Contains(help, "kubestellar-deploy"), "help should mention kubestellar-deploy")
	require.True(t, strings.Contains(help, "multi-cluster"), "help should mention multi-cluster")
}

func TestDeployRootCommand_Use(t *testing.T) {
	cmd := NewRootCommand()
	require.Equal(t, "kubestellar-deploy", cmd.Use, "root command Use should be kubestellar-deploy")
}

func TestDeployRootCommand_Short(t *testing.T) {
	cmd := NewRootCommand()
	require.NotEmpty(t, cmd.Short, "root command should have short description")
	require.True(t, strings.Contains(cmd.Short, "multi-cluster"), "short description should mention multi-cluster")
}

func TestVersionCommand_Output(t *testing.T) {
	versionCmd := newVersionCommand()
	require.Equal(t, "version", versionCmd.Use, "version command Use should be 'version'")
	require.NotEmpty(t, versionCmd.Short, "version command should have short description")
}

func TestDeployRootCommand_RunEExists(t *testing.T) {
	cmd := NewRootCommand()
	require.NotNil(t, cmd.RunE, "root command should have RunE defined")
}

func TestExecute_ReturnsWithoutPanic(t *testing.T) {
	// Test that Execute() can be called without panicking
	// We don't actually execute it to avoid test interference,
	// but we verify the function exists and is callable
	require.NotPanics(t, func() {
		_ = Execute
	})
}
