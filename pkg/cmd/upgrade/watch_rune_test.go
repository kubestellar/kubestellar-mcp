package upgrade

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

// TestNewWatchCommand_RunEInvokesWatchUpgrade covers the RunE closure body
// (watch.go:66) by invoking cmd.RunE with a ConfigFlags pointing at a
// non-existent kubeconfig, which causes watchUpgrade to return early with the
// "failed to load kubeconfig" error. This exercises the closure that delegates
// to watchUpgrade, previously uncovered.
func TestNewWatchCommand_RunEInvokesWatchUpgrade(t *testing.T) {
	badPath := "/nonexistent/kubeconfig/for/quality-coverage-test"
	configFlags := genericclioptions.NewConfigFlags(true)
	configFlags.KubeConfig = &badPath

	cmd := NewWatchCommand(configFlags)
	require.NotNil(t, cmd.RunE)

	cmd.SetContext(context.Background())
	err := cmd.RunE(cmd, []string{})
	require.Error(t, err)
	require.True(t,
		strings.Contains(err.Error(), "failed to load kubeconfig") ||
			strings.Contains(err.Error(), "not an OpenShift cluster") ||
			strings.Contains(err.Error(), "no such file"),
		"unexpected error: %v", err)
}
