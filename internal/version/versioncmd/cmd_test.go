package versioncmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubestellar/kubestellar-mcp/internal/version"
)

// TestNew_MetadataMatchesCobraConventions guards the subcommand shape so a
// future edit that drops the Run function or renames the Use string breaks
// here instead of at release time in the CLI banner.
func TestNew_MetadataMatchesCobraConventions(t *testing.T) {
	cmd := New("kubestellar-example")

	require.Equal(t, "version", cmd.Use)
	require.NotEmpty(t, cmd.Short)
	require.NotNil(t, cmd.Run, "version command must have a Run function")
}

// TestNew_RunPrintsLdflagsInjectedVersion is the whole reason this shared
// helper exists: it fires the Run body with the concrete internal/version
// vars and asserts the banner matches the ldflags-injection contract that
// kubestellar-deploy previously ignored (see architect finding).
func TestNew_RunPrintsLdflagsInjectedVersion(t *testing.T) {
	origVersion, origBuildDate, origGitCommit := version.Version, version.BuildDate, version.GitCommit
	t.Cleanup(func() {
		version.Version, version.BuildDate, version.GitCommit = origVersion, origBuildDate, origGitCommit
	})
	version.Version = "v1.2.3-test"
	version.BuildDate = "2026-09-23T00:00:00Z"
	version.GitCommit = "abc1234"

	cmd := New("kubestellar-example")

	// fmt.Println/Printf inside Run writes directly to os.Stdout, not
	// cmd.OutOrStdout, so redirecting the cobra output writer isn't enough.
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	cmd.Run(cmd, []string{})

	require.NoError(t, w.Close())
	os.Stdout = origStdout

	var buf bytes.Buffer
	_, err = io.Copy(&buf, r)
	require.NoError(t, err)
	out := buf.String()

	require.True(t, strings.Contains(out, "kubestellar-example version v1.2.3-test"),
		"expected banner with injected version, got %q", out)
	require.True(t, strings.Contains(out, "Build date: 2026-09-23T00:00:00Z"),
		"expected build date line, got %q", out)
	require.True(t, strings.Contains(out, "Git commit: abc1234"),
		"expected git commit line, got %q", out)
}

// TestNew_RunAcceptsNilArgs mirrors the defensive test that lived in
// pkg/deploy/cmd (nil-args non-panic guard) so the property survives the
// extraction into this shared helper.
func TestNew_RunAcceptsNilArgs(t *testing.T) {
	cmd := New("kubestellar-example")

	origStdout := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w
	defer func() {
		_ = w.Close()
		os.Stdout = origStdout
	}()

	require.NotPanics(t, func() {
		cmd.Run(cmd, nil)
	})
}
