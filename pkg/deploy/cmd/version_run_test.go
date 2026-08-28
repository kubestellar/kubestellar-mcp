package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Guards the Run function body of newVersionCommand, previously
// uncovered (function-level coverage 50% because only the metadata
// — Use / Short — was inspected). If a regression changed the
// printed banner or wired a Run that panics on nil args, we'd have
// no signal from the existing suite.
func TestVersionCommand_RunPrintsBanner(t *testing.T) {
	cmd := newVersionCommand()
	require.NotNil(t, cmd.Run, "version command should have a Run function")

	// Capture stdout: newVersionCommand's Run uses fmt.Println,
	// which writes directly to os.Stdout — not cmd.OutOrStdout —
	// so redirecting cmd's output writer isn't sufficient.
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

	require.True(
		t,
		strings.Contains(out, "kubestellar-deploy version"),
		"expected version banner in stdout, got %q", out,
	)
}

// Extra defensive: passing nil args (as cobra sometimes does when
// no positional args are given) must not panic. Guards against a
// future Run body that unconditionally indexes args.
func TestVersionCommand_RunAcceptsNilArgs(t *testing.T) {
	cmd := newVersionCommand()
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
