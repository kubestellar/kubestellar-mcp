package helm

import (
	"bytes"
	"context"
	"io"
	"os/exec"
)

// execHelm runs the helm CLI with the given arguments, capturing stdout and
// stderr into separate buffers. It centralizes the
// exec.CommandContext/bytes.Buffer boilerplate that was previously
// duplicated across helmInstall, helmList, helmReleaseExists, helmRollback,
// and helmUninstall. stdin may be nil when the invocation has no input to
// pipe in (e.g. no --values -).
func execHelm(ctx context.Context, stdin io.Reader, args ...string) (stdout, stderr bytes.Buffer, err error) {
	cmd := exec.CommandContext(ctx, "helm", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if stdin != nil {
		cmd.Stdin = stdin
	}
	err = cmd.Run()
	return stdout, stderr, err
}
