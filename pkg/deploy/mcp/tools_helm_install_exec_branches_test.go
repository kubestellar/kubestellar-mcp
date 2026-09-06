package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests target the three uncovered arms of Server.helmInstall in
// tools_helm.go:385 that the existing test
// TestHandleHelmInstallDiscoversClustersAndPassesFlags does not exercise
// (that test uses dry_run=true, which short-circuits to the "would-install"
// arm before the install/upgrade/failed status dispatch).
//
// Arms exercised here:
//   - revalidateHelmHosts rejects (early return, Status "failed")
//   - non-dry-run success, stdout contains "has been upgraded"  → "upgraded"
//   - non-dry-run success, stdout without that substring        → "installed"
//   - non-dry-run exec error                                    → "failed", Message from stderr

func writeExecScript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func TestHelmInstall_UpgradedStatus(t *testing.T) {
	_ = setupFakeHelm(t)
	// The fake helm echoes $FAKE_HELM_UPGRADE_STDOUT on the `upgrade` arm.
	// Set stdout to contain the "has been upgraded" marker so helmInstall
	// classifies the result as "upgraded".
	t.Setenv("FAKE_HELM_UPGRADE_STDOUT", `Release "demo" has been upgraded. Happy Helming!`)
	setHelmMockResolver(t, func(_ string) ([]string, error) {
		return []string{"93.184.216.34"}, nil // public IP, not blocked
	})

	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
	})

	res := server.helmInstall(context.Background(), "alpha", "demo",
		"oci://example/demo", "apps",
		nil, "", "", "", false, "", false /*dryRun*/)

	if res.Status != "upgraded" {
		t.Fatalf("Status = %q, want %q; message=%q", res.Status, "upgraded", res.Message)
	}
	if !strings.Contains(res.Message, "has been upgraded") {
		t.Errorf("Message missing stdout marker: %q", res.Message)
	}
	if res.Cluster != "alpha" || res.ReleaseName != "demo" || res.Namespace != "apps" {
		t.Errorf("unexpected identity fields: %#v", res)
	}
}

func TestHelmInstall_InstalledStatus(t *testing.T) {
	_ = setupFakeHelm(t)
	// stdout does NOT contain "has been upgraded" → default "installed" arm.
	t.Setenv("FAKE_HELM_UPGRADE_STDOUT", `Release "demo" has been installed. Happy Helming!`)
	setHelmMockResolver(t, func(_ string) ([]string, error) {
		return []string{"93.184.216.34"}, nil
	})

	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
	})

	res := server.helmInstall(context.Background(), "alpha", "demo",
		"oci://example/demo", "apps",
		nil, "", "", "", false, "", false)

	if res.Status != "installed" {
		t.Fatalf("Status = %q, want %q; message=%q", res.Status, "installed", res.Message)
	}
}

func TestHelmInstall_FailedStatus_ExecError(t *testing.T) {
	// Don't call setupFakeHelm — build a helm binary that exits non-zero
	// so helmInstall lands in the `err != nil` arm and pulls Message from stderr.
	failDir := t.TempDir()
	writeExecScript(t, filepath.Join(failDir, "helm"),
		"#!/bin/sh\necho 'boom: install failed' 1>&2\nexit 1\n")
	t.Setenv("PATH", failDir+":"+os.Getenv("PATH"))

	setHelmMockResolver(t, func(_ string) ([]string, error) {
		return []string{"93.184.216.34"}, nil
	})

	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
	})

	res := server.helmInstall(context.Background(), "alpha", "demo",
		"oci://example/demo", "apps",
		nil, "", "", "", false, "", false)

	if res.Status != "failed" {
		t.Fatalf("Status = %q, want %q; message=%q", res.Status, "failed", res.Message)
	}
	if !strings.Contains(res.Message, "boom: install failed") {
		t.Errorf("Message missing stderr from failing helm: %q", res.Message)
	}
}

// TestHelmInstall_PreExecSSRFReCheckFails exercises the early-return arm
// where revalidateHelmHosts rejects the chart/repo hostnames (rebound to
// a blocked IP between input validation and exec).
func TestHelmInstall_PreExecSSRFReCheckFails(t *testing.T) {
	// Resolver returns a loopback IP → revalidateHelmHosts fails.
	setHelmMockResolver(t, func(_ string) ([]string, error) {
		return []string{"127.0.0.1"}, nil
	})

	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
	})

	res := server.helmInstall(context.Background(), "alpha", "demo",
		"https://charts.example.invalid/demo", "apps",
		nil, "", "", "https://charts.example.invalid", false, "", false)

	if res.Status != "failed" {
		t.Fatalf("Status = %q, want %q; message=%q", res.Status, "failed", res.Message)
	}
	if !strings.Contains(res.Message, "pre-exec SSRF re-check failed") {
		t.Errorf("Message missing SSRF marker: %q", res.Message)
	}
}
