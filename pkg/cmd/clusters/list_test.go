package clusters

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func TestNewListCommand_DefaultSourceAndFlagParsing(t *testing.T) {
	configFlags := genericclioptions.NewConfigFlags(true)
	cmd := newListCommand(configFlags)

	require.Equal(t, "list", cmd.Use)
	require.Equal(t, "all", cmd.Flag("source").DefValue)

	require.NoError(t, cmd.ParseFlags([]string{"--source=kubestellar"}))
	require.Equal(t, "kubestellar", cmd.Flag("source").Value.String())
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "short string unchanged",
			input:  "abc",
			maxLen: 10,
			want:   "abc",
		},
		{
			name:   "string exactly max len unchanged",
			input:  "abcdef",
			maxLen: 6,
			want:   "abcdef",
		},
		{
			name:   "long string gets ellipsis",
			input:  "abcdefghijklmnopqrstuvwxyz",
			maxLen: 10,
			want:   "abcdefg...",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, truncateString(tt.input, tt.maxLen))
		})
	}
}
