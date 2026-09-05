package ai

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

// TestQueryCommand_ReturnsClaudeQueryError covers the previously-uncovered
// error arm at pkg/cmd/ai/query.go:151-152 where client.Query fails and
// run() wraps the error with "failed to get response". The existing
// TestQueryCommand_ReturnsClaudeClientCreationError hits the earlier
// newClaudeQueryClient error path but no test drove the Claude query
// itself to failure.
func TestQueryCommand_ReturnsClaudeQueryError(t *testing.T) {
	queryErr := errors.New("upstream 500")
	client := &fakeClaudeQueryClient{err: queryErr}
	discoverer := &fakeClusterDiscoverer{}
	_, restore := stubQueryDependencies(t, client, discoverer)
	defer restore()

	configFlags := genericclioptions.NewConfigFlags(true)
	cmd := NewQueryCommand(configFlags)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"list", "pods"})

	err := cmd.ExecuteContext(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to get response")
	require.ErrorIs(t, err, queryErr)
}
