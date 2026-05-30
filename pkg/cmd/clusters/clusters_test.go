package clusters

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func TestNewClustersCommand_AddsExpectedSubcommands(t *testing.T) {
	configFlags := genericclioptions.NewConfigFlags(true)
	cmd := NewClustersCommand(configFlags)

	require.Equal(t, "clusters", cmd.Use)

	tests := []struct {
		name       string
		commandUse string
	}{
		{name: "list subcommand", commandUse: "list"},
		{name: "health subcommand", commandUse: "health [cluster-name]"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			found := false
			for _, sub := range cmd.Commands() {
				if sub.Use == tt.commandUse {
					found = true
					break
				}
			}
			require.Truef(t, found, "expected subcommand %q", tt.commandUse)
		})
	}
}
