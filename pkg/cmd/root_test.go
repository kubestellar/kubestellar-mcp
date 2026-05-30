package cmd

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRootCommand_HasExpectedFlags(t *testing.T) {
	tests := []struct {
		name     string
		flagName string
	}{
		{name: "mcp-server flag", flagName: "mcp-server"},
		{name: "all-clusters flag", flagName: "all-clusters"},
		{name: "target-cluster flag", flagName: "target-cluster"},
		{name: "context flag", flagName: "context"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			flag := rootCmd.PersistentFlags().Lookup(tt.flagName)
			require.NotNilf(t, flag, "expected flag %q to be registered", tt.flagName)
		})
	}
}

func TestRootCommand_HasExpectedSubcommands(t *testing.T) {
	tests := []struct {
		name        string
		commandName string
	}{
		{name: "clusters subcommand", commandName: "clusters"},
		{name: "query subcommand", commandName: "query"},
		{name: "watch-upgrade subcommand", commandName: "watch-upgrade"},
		{name: "version subcommand", commandName: "version"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			found := false
			for _, sub := range rootCmd.Commands() {
				if sub.Name() == tt.commandName {
					found = true
					break
				}
			}
			require.Truef(t, found, "expected subcommand %q to be registered", tt.commandName)
		})
	}
}

func TestIsNaturalLanguageQuery_SubcommandsFalse(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "empty args", args: []string{}, want: false},
		{name: "clusters command", args: []string{"clusters"}, want: false},
		{name: "query command", args: []string{"query"}, want: false},
		{name: "watch-upgrade command", args: []string{"watch-upgrade"}, want: false},
		{name: "version command", args: []string{"version"}, want: false},
		{name: "help command", args: []string{"help"}, want: false},
		{name: "flag arg", args: []string{"--all-clusters"}, want: false},
		{name: "short flag arg", args: []string{"-h"}, want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := isNaturalLanguageQuery(tt.args)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestIsNaturalLanguageQuery_NaturalLanguageTrue(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "simple question", args: []string{"show", "me", "pods"}},
		{name: "single word", args: []string{"status"}},
		{name: "natural sentence", args: []string{"what", "pods", "are", "failing"}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := isNaturalLanguageQuery(tt.args)
			require.True(t, got, "expected args to be recognized as natural language")
		})
	}
}

func TestRootCommand_HelpOutput(t *testing.T) {
	help := rootCmd.Long
	require.NotEmpty(t, help, "root command should have help text")
	require.True(t, strings.Contains(help, "kubestellar-ops"), "help should mention kubestellar-ops")
	require.True(t, strings.Contains(help, "multi-cluster"), "help should mention multi-cluster")
}

func TestRootCommand_FlagsParsedCorrectly(t *testing.T) {
	tests := []struct {
		name      string
		flagName  string
		flagValue string
		checkFunc func(t *testing.T)
	}{
		{
			name:      "mcp-server boolean flag",
			flagName:  "mcp-server",
			flagValue: "true",
			checkFunc: func(t *testing.T) {
				flag := rootCmd.PersistentFlags().Lookup("mcp-server")
				require.NotNil(t, flag)
				require.Equal(t, "bool", flag.Value.Type())
			},
		},
		{
			name:      "all-clusters boolean flag",
			flagName:  "all-clusters",
			flagValue: "true",
			checkFunc: func(t *testing.T) {
				flag := rootCmd.PersistentFlags().Lookup("all-clusters")
				require.NotNil(t, flag)
				require.Equal(t, "bool", flag.Value.Type())
			},
		},
		{
			name:      "target-cluster string flag",
			flagName:  "target-cluster",
			flagValue: "test-cluster",
			checkFunc: func(t *testing.T) {
				flag := rootCmd.PersistentFlags().Lookup("target-cluster")
				require.NotNil(t, flag)
				require.Equal(t, "string", flag.Value.Type())
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			tt.checkFunc(t)
		})
	}
}
