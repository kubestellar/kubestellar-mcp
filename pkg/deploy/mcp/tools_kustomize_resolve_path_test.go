package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resolveKustomizePath is the security boundary for kustomize handlers:
// it clamps user-supplied paths to a small allow-list (working directory
// or system temp directory) after symlink resolution, so an attacker
// cannot smuggle a path like /etc/… or ../../etc through the kustomize
// tools. Only indirect coverage existed (via handleKustomizeBuild) — this
// file adds direct unit tests, including the missing error branches.

func TestResolveKustomizePath_AcceptsPathUnderWorkingDirectory(t *testing.T) {
	// Working directory is well-defined during 'go test'; resolveKustomizePath
	// resolves it via EvalSymlinks so equivalent paths compare as equal.
	workingDir, err := os.Getwd()
	require.NoError(t, err)

	// Create a file under the working directory (t.TempDir won't necessarily
	// be under it, so use a scratch subdir).
	scratchDir, err := os.MkdirTemp(workingDir, "kustomize-cwd-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(scratchDir) })

	got, err := resolveKustomizePath(scratchDir)
	require.NoError(t, err)

	resolvedWD, err := filepath.EvalSymlinks(workingDir)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(got, resolvedWD),
		"resolved path %q must begin with the working directory prefix %q", got, resolvedWD)
}

func TestResolveKustomizePath_AcceptsPathUnderTempDirectory(t *testing.T) {
	// Point TMPDIR at an isolated sub-tree so we can predict what os.TempDir()
	// returns inside the function.
	isolated := t.TempDir()
	t.Setenv("TMPDIR", isolated)

	// Create a directory under the (post-eval) tempdir.
	scratchDir, err := os.MkdirTemp(isolated, "kustomize-tmp-*")
	require.NoError(t, err)

	got, err := resolveKustomizePath(scratchDir)
	require.NoError(t, err)

	resolvedTmp, err := filepath.EvalSymlinks(isolated)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(got, resolvedTmp),
		"resolved path %q must begin with the temp directory prefix %q", got, resolvedTmp)
}

func TestResolveKustomizePath_FailsWhenPathDoesNotExist(t *testing.T) {
	// This exercises the `failed to resolve path` branch — the EvalSymlinks
	// call at the top of the function errors because the target does not
	// exist on disk.
	missing := filepath.Join(t.TempDir(), "definitely-does-not-exist")

	got, err := resolveKustomizePath(missing)
	require.Error(t, err)
	assert.Empty(t, got)
	assert.Contains(t, err.Error(), "failed to resolve path")
	assert.Contains(t, err.Error(), missing,
		"error must include the offending path to aid debugging")
}

func TestResolveKustomizePath_RejectsPathOutsideAllowedDirectories(t *testing.T) {
	// Isolate TMPDIR to a narrow tree so the "outside" dir does not fall
	// under os.TempDir() by accident.
	isolatedTmp := t.TempDir()
	t.Setenv("TMPDIR", isolatedTmp)

	workingDir, err := os.Getwd()
	require.NoError(t, err)

	// Create the outside directory at the parent of the working directory,
	// which is neither under CWD nor under our isolated TMPDIR.
	outsideRoot := filepath.Dir(workingDir)
	outsideDir, err := os.MkdirTemp(outsideRoot, "kustomize-outside-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(outsideDir) })

	got, err := resolveKustomizePath(outsideDir)
	require.Error(t, err)
	assert.Empty(t, got)
	assert.Contains(t, err.Error(), "outside allowed directories")
}

func TestResolveKustomizePath_ResolvesSymlinkAndValidatesTarget(t *testing.T) {
	// A symlink under CWD that points to a directory OUTSIDE the allowed
	// bases must be rejected — resolveKustomizePath calls EvalSymlinks before
	// the allow-list check. This is the exact bypass attempt guarded by the
	// resolvedPath / EvalSymlinks pair; regressing to a pre-symlink check
	// would silently re-open the vulnerability.
	isolatedTmp := t.TempDir()
	t.Setenv("TMPDIR", isolatedTmp)

	workingDir, err := os.Getwd()
	require.NoError(t, err)

	outsideRoot := filepath.Dir(workingDir)
	outsideDir, err := os.MkdirTemp(outsideRoot, "kustomize-outside-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(outsideDir) })

	linkPath := filepath.Join(t.TempDir(), "symlink-to-outside")
	require.NoError(t, os.Symlink(outsideDir, linkPath))
	// Note: linkPath itself is under TMPDIR (t.TempDir returns a subdir of
	// os.TempDir()), but EvalSymlinks resolves it to outsideDir which is
	// outside every allowed base — the check must still reject.

	got, err := resolveKustomizePath(linkPath)
	require.Error(t, err)
	assert.Empty(t, got)
	assert.Contains(t, err.Error(), "outside allowed directories",
		"symlink pointing outside allowed bases must be rejected after EvalSymlinks")
}

func TestResolveKustomizePath_AcceptsCurrentDirectoryDot(t *testing.T) {
	// The bare "." path is a common kustomize idiom (a kustomization.yaml
	// at the CWD). filepath.Rel(cwd, cwd) returns ".", not "..", so this
	// must succeed — regression guard against tightening the check to
	// reject "." by accident.
	got, err := resolveKustomizePath(".")
	require.NoError(t, err)

	workingDir, err := os.Getwd()
	require.NoError(t, err)
	resolvedWD, err := filepath.EvalSymlinks(workingDir)
	require.NoError(t, err)
	assert.Equal(t, resolvedWD, got)
}

func TestResolveKustomizePath_CleansTraversalBeforeResolve(t *testing.T) {
	// filepath.Clean collapses "foo/../bar" to "bar" before EvalSymlinks
	// looks at the path. A relative path that walks back into CWD via
	// "…/subdir/.." must still resolve correctly.
	workingDir, err := os.Getwd()
	require.NoError(t, err)

	scratchDir, err := os.MkdirTemp(workingDir, "kustomize-cwd-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(scratchDir) })

	base := filepath.Base(scratchDir)
	// e.g. "./<scratchDir>/subdir/.." should Clean to "./<scratchDir>"
	roundTrip := "./" + base + "/subdir/.."

	got, err := resolveKustomizePath(roundTrip)
	require.NoError(t, err)

	resolvedWD, err := filepath.EvalSymlinks(workingDir)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(got, resolvedWD))
}

func TestResolveKustomizePath_FailsWhenTempDirCannotBeResolved(t *testing.T) {
	// os.TempDir() returns $TMPDIR when set. If TMPDIR points at a path
	// that does not exist on disk, EvalSymlinks(os.TempDir()) fails and
	// resolveKustomizePath returns the "failed to resolve temp directory"
	// error. This arm was previously uncovered — a regression here would
	// mean the security allow-list silently degrades to a single-base
	// check whenever the temp base cannot be probed, so we lock the
	// behavior down with a targeted test.
	//
	// Use a path pointing INTO an existing tempdir but with a nonexistent
	// leaf, guaranteeing EvalSymlinks fails without depending on any
	// system-specific "/nonexistent" prefix.
	realTmp := t.TempDir()
	brokenTmp := filepath.Join(realTmp, "definitely-not-a-real-tmpdir")
	t.Setenv("TMPDIR", brokenTmp)

	// The path we ask about must still exist on disk (so the upstream
	// EvalSymlinks(absPath) call succeeds and we actually reach the
	// TempDir resolution). Use the real tempdir the harness gave us.
	got, err := resolveKustomizePath(realTmp)
	require.Error(t, err)
	assert.Empty(t, got)
	assert.Contains(t, err.Error(), "failed to resolve temp directory",
		"broken TMPDIR must surface as a temp-dir resolution error, not a false accept or a different error")
}

func TestResolveKustomizePath_FailsWhenWorkingDirIsUnavailable(t *testing.T) {
	// If os.Getwd() fails (because the process's current directory was
	// unlinked out from under it), resolveKustomizePath cannot determine
	// one of its two allow-list bases and must return the "failed to
	// determine working directory" error. This arm was previously
	// uncovered.
	//
	// Reproduce the state safely: create a scratch dir, chdir into it via
	// t.Chdir (which restores CWD on cleanup), then remove the scratch dir
	// so a subsequent Getwd cannot resolve it. We pass an ABSOLUTE path
	// that DOES exist so the earlier EvalSymlinks(absPath) succeeds and
	// execution reaches the os.Getwd() call we want to fail.
	scratchParent := t.TempDir()
	scratchDir := filepath.Join(scratchParent, "will-be-removed")
	require.NoError(t, os.Mkdir(scratchDir, 0o755))
	t.Chdir(scratchDir)
	require.NoError(t, os.Remove(scratchDir))

	// scratchParent still exists, so EvalSymlinks succeeds on it and we
	// reach the Getwd branch.
	got, err := resolveKustomizePath(scratchParent)
	require.Error(t, err)
	assert.Empty(t, got)
	assert.Contains(t, err.Error(), "failed to determine working directory",
		"unlinked CWD must surface as a working-directory error, not leak the Getwd errno detail")
}
