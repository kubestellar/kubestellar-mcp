package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests target the two uncovered arms of Server.helmRollback in
// tools_helm.go:795 that the existing rollback tests do not exercise.
// TestHandleHelmRollback_DefaultsNamespaceToDefault covers the dry_run=true
// "would-rollback" arm; TestHandleHelmRollback_NoClustersHaveRelease fails
// out before ever reaching helmRollback. The two remaining arms are:
//
//   1. non-dry-run + exec success       -> Status "rolled-back", stdout in Message
//   2. non-dry-run + exec error         -> Status "failed",      stderr in Message
//
// helmRollback is called directly (rather than through handleHelmRollback)
// so the tests can substitute a purpose-built helm binary on PATH without
// needing the discovery / helmReleaseExists dance.

func TestHelmRollback_RolledBackStatus_NonDryRunSuccess(t *testing.T) {
	// Build a helm binary that succeeds on `rollback` and prints a
	// distinctive stdout marker so we can assert Message is stdout,
	// not the empty string or stderr.
	dir := t.TempDir()
	writeExecScript(t, filepath.Join(dir, "helm"),
		"#!/bin/sh\n"+
			"if [ \"$1\" = \"rollback\" ]; then\n"+
			"  echo 'Rollback was a success! Happy Helming!'\n"+
			"  exit 0\n"+
			"fi\n"+
			"exit 1\n")
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
	})

	res := server.helmRollback(context.Background(), "alpha", "webapp", "prod", 3, false)

	if res.Status != "rolled-back" {
		t.Fatalf("Status = %q, want %q; message=%q", res.Status, "rolled-back", res.Message)
	}
	if res.Cluster != "alpha" || res.ReleaseName != "webapp" || res.Namespace != "prod" {
		t.Errorf("identity fields: cluster=%q release=%q namespace=%q", res.Cluster, res.ReleaseName, res.Namespace)
	}
	if !strings.Contains(res.Message, "Rollback was a success") {
		t.Errorf("Message missing stdout marker: %q", res.Message)
	}
}

// TestHelmRollback_FailedStatus_ExecError exercises the `err != nil` arm at
// tools_helm.go:826-834. The fake helm exits 1 on `rollback` and writes a
// diagnostic to stderr; helmRollback must return Status "failed" with the
// stderr text (not stdout) in Message so operators can see why the rollback
// aborted.
func TestHelmRollback_FailedStatus_ExecError(t *testing.T) {
	dir := t.TempDir()
	writeExecScript(t, filepath.Join(dir, "helm"),
		"#!/bin/sh\n"+
			"if [ \"$1\" = \"rollback\" ]; then\n"+
			"  echo 'rollback: release has no rollback history' 1>&2\n"+
			"  exit 1\n"+
			"fi\n"+
			"exit 1\n")
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
	})

	res := server.helmRollback(context.Background(), "alpha", "webapp", "prod", 0, false)

	if res.Status != "failed" {
		t.Fatalf("Status = %q, want %q; message=%q", res.Status, "failed", res.Message)
	}
	if res.Cluster != "alpha" || res.ReleaseName != "webapp" || res.Namespace != "prod" {
		t.Errorf("identity fields: cluster=%q release=%q namespace=%q", res.Cluster, res.ReleaseName, res.Namespace)
	}
	if !strings.Contains(res.Message, "no rollback history") {
		t.Errorf("Message missing stderr marker: %q", res.Message)
	}
}

// TestHelmRollback_FailedStatus_DryRunError verifies that when dry_run=true
// is requested but the underlying helm invocation still errors (e.g. the
// release is malformed and `helm rollback --dry-run` itself fails), the
// handler falls through to the "failed" arm rather than reporting the
// misleading "would-rollback" outcome. This guards the `dryRun && err == nil`
// conjunction in tools_helm.go:815 against a regression that dropped the
// `err == nil` requirement and silently reported success on a broken chart.
func TestHelmRollback_FailedStatus_DryRunError(t *testing.T) {
	dir := t.TempDir()
	writeExecScript(t, filepath.Join(dir, "helm"),
		"#!/bin/sh\n"+
			"if [ \"$1\" = \"rollback\" ]; then\n"+
			"  echo 'dry-run planning error' 1>&2\n"+
			"  exit 1\n"+
			"fi\n"+
			"exit 1\n")
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
	})

	res := server.helmRollback(context.Background(), "alpha", "webapp", "prod", 0, true)

	if res.Status != "failed" {
		t.Fatalf("Status = %q, want %q; message=%q", res.Status, "failed", res.Message)
	}
	if !strings.Contains(res.Message, "dry-run planning error") {
		t.Errorf("Message missing stderr marker: %q", res.Message)
	}
}
