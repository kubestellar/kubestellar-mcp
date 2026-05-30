package clusters

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func TestNewHealthCommand_DefaultFlagAndArgs(t *testing.T) {
	configFlags := genericclioptions.NewConfigFlags(true)
	cmd := newHealthCommand(configFlags)

	require.Equal(t, "health [cluster-name]", cmd.Use)
	require.Equal(t, "false", cmd.Flag("all-clusters").DefValue)

	require.NoError(t, cmd.ParseFlags([]string{"--all-clusters"}))
	require.Equal(t, "true", cmd.Flag("all-clusters").Value.String())
}
