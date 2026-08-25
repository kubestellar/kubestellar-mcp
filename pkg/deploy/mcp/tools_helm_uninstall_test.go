package mcp

// Coverage-focused tests for handleHelmUninstall / helmUninstall.
//
// Before this file, the existing tools_helm_test.go only exercised the
// dry-run path through handleHelmUninstall (dry_run: true), which returned
// early from helmUninstall with "would-uninstall" without ever invoking the
// fake helm binary. Coverage numbers:
//
//   helmUninstall          6.2%   (only the dry-run early return)
//   handleHelmUninstall    77.8%  (missing validation + not-found + no-clusters paths)
//
// These tests exercise:
//   1. Non-dry-run success — helm exits 0, result Status="uninstalled",
//      Message carries helm stdout, cluster/namespace flags are threaded
//      through to the exec'd command.
//   2. Non-dry-run failure — helm exits 1, result Status="failed",
//      Message carries helm stderr; NOT propagated as a Go error (the
//      handler aggregates results across clusters).
//   3. Explicit clusters parameter bypasses helmReleaseExists discovery.
//   4. Missing release_name — validation error.
//   5. Invalid (system) namespace — ValidateNamespace rejects kube-system.
//   6. release not found in any cluster — DiscoverClusters returns hits
//      but helmReleaseExists returns false for all.
//   7. Malformed JSON args — top-level unmarshal error.
//   8. Invalid release_name (flag-injection guard, #269).
//
// Fake-helm shim: extended with FAKE_HELM_UNINSTALL_FAIL_CLUSTERS so we can
// force a non-zero exit on a specific cluster. Successful uninstalls now
// echo `release "<name>" uninstalled` so the tests can assert the Message
// contents.

import (
	"context"
	"strings"
	"testing"
)

// ─── Non-dry-run success ────────────────────────────────────────────────

func TestHelmUninstall_RealUninstallSuccess(t *testing.T) {
	logFile := setupFakeHelm(t)
	// Make helmReleaseExists succeed for "alpha" so discovery picks it up.
	t.Setenv("FAKE_HELM_STATUS_CLUSTERS", "alpha")

	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
	})
	args := mustMarshalJSON(t, map[string]interface{}{
		"release_name": "webapp",
		"namespace":    "production",
		"dry_run":      false,
	})

	got, err := server.handleHelmUninstall(context.Background(), args)
	if err != nil {
		t.Fatalf("handleHelmUninstall() error = %v", err)
	}

	result := got.(map[string]interface{})
	if result["successCount"].(int) != 1 || result["totalClusters"].(int) != 1 {
		t.Fatalf("unexpected counts: %#v", result)
	}
	if result["dryRun"].(bool) != false {
		t.Fatalf("dryRun = true, want false")
	}
	results := result["results"].([]HelmResult)
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].Status != "uninstalled" {
		t.Fatalf("Status = %q, want %q", results[0].Status, "uninstalled")
	}
	if !strings.Contains(results[0].Message, `release "webapp" uninstalled`) {
		t.Fatalf("Message = %q, want it to carry helm stdout", results[0].Message)
	}
	if results[0].Cluster != "alpha" || results[0].Namespace != "production" || results[0].ReleaseName != "webapp" {
		t.Fatalf("result identity fields wrong: %#v", results[0])
	}

	// Verify the fake helm actually saw the uninstall subcommand with the
	// right cluster + namespace, i.e. helmUninstall built cmdArgs correctly.
	log := readLogFile(t, logFile)
	if !strings.Contains(log, "cmd=uninstall") {
		t.Fatalf("fake helm did not see uninstall command, log:\n%s", log)
	}
	if !strings.Contains(log, "cluster=alpha") {
		t.Fatalf("fake helm did not see --kube-context alpha, log:\n%s", log)
	}
	if !strings.Contains(log, "namespace=production") {
		t.Fatalf("fake helm did not see --namespace production, log:\n%s", log)
	}
}

// ─── Non-dry-run failure ────────────────────────────────────────────────

func TestHelmUninstall_RealUninstallFailure(t *testing.T) {
	logFile := setupFakeHelm(t)
	t.Setenv("FAKE_HELM_STATUS_CLUSTERS", "beta")
	t.Setenv("FAKE_HELM_UNINSTALL_FAIL_CLUSTERS", "beta")
	t.Setenv("FAKE_HELM_UNINSTALL_FAIL_MSG", "Error: release: not found")

	server := newHelmTestServer(t, map[string]string{
		"beta": "https://beta.example.com",
	})
	args := mustMarshalJSON(t, map[string]interface{}{
		"release_name": "webapp",
		"namespace":    "staging",
		"dry_run":      false,
	})

	got, err := server.handleHelmUninstall(context.Background(), args)
	if err != nil {
		t.Fatalf("handleHelmUninstall() error = %v; want nil (per-cluster failure is a Result, not a Go error)", err)
	}

	result := got.(map[string]interface{})
	// One cluster targeted, zero succeeded.
	if result["successCount"].(int) != 0 || result["totalClusters"].(int) != 1 {
		t.Fatalf("unexpected counts: %#v", result)
	}
	results := result["results"].([]HelmResult)
	if len(results) != 1 || results[0].Status != "failed" {
		t.Fatalf("Status = %q, want %q; results=%#v", results[0].Status, "failed", results)
	}
	if !strings.Contains(results[0].Message, "release: not found") {
		t.Fatalf("Message = %q, want it to carry helm stderr", results[0].Message)
	}
	_ = logFile
}

// ─── Explicit clusters bypass discovery ─────────────────────────────────

func TestHelmUninstall_ExplicitClustersSkipsReleaseDiscovery(t *testing.T) {
	logFile := setupFakeHelm(t)
	// Deliberately DO NOT set FAKE_HELM_STATUS_CLUSTERS — so if the handler
	// were relying on helmReleaseExists it would exclude everything.
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
		"beta":  "https://beta.example.com",
	})
	args := mustMarshalJSON(t, map[string]interface{}{
		"release_name": "myrelease",
		"namespace":    "default",
		"dry_run":      false,
		"clusters":     []string{"alpha", "beta"},
	})

	got, err := server.handleHelmUninstall(context.Background(), args)
	if err != nil {
		t.Fatalf("handleHelmUninstall() error = %v", err)
	}

	result := got.(map[string]interface{})
	targets := result["targetClusters"].([]string)
	if len(targets) != 2 {
		t.Fatalf("targetClusters = %v, want 2 clusters (discovery must be skipped when clusters: is set)", targets)
	}
	if result["successCount"].(int) != 2 {
		t.Fatalf("successCount = %d, want 2", result["successCount"].(int))
	}
	log := readLogFile(t, logFile)
	// Neither cluster should have had a status probe.
	if strings.Contains(log, "cmd=status") {
		t.Fatalf("helmReleaseExists (status) invoked despite explicit clusters, log:\n%s", log)
	}
}

// ─── Validation error paths (handleHelmUninstall) ───────────────────────

func TestHandleHelmUninstall_MissingReleaseName(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})
	args := mustMarshalJSON(t, map[string]interface{}{
		"namespace": "default",
	})
	_, err := server.handleHelmUninstall(context.Background(), args)
	if err == nil || !strings.Contains(err.Error(), "release_name is required") {
		t.Fatalf("err = %v, want 'release_name is required'", err)
	}
}

func TestHandleHelmUninstall_SystemNamespaceRejected(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})
	args := mustMarshalJSON(t, map[string]interface{}{
		"release_name": "myrelease",
		"namespace":    "kube-system",
	})
	_, err := server.handleHelmUninstall(context.Background(), args)
	if err == nil || !strings.Contains(err.Error(), "invalid namespace") {
		t.Fatalf("err = %v, want 'invalid namespace' for kube-system", err)
	}
}

func TestHandleHelmUninstall_InvalidReleaseNameFlagInjection(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})
	args := mustMarshalJSON(t, map[string]interface{}{
		// Leading '-' would look like a helm flag if forwarded.
		"release_name": "-rm-rf",
		"namespace":    "default",
	})
	_, err := server.handleHelmUninstall(context.Background(), args)
	if err == nil {
		t.Fatalf("expected validation error on release_name '-rm-rf'")
	}
}

func TestHandleHelmUninstall_NoClustersHaveRelease(t *testing.T) {
	_ = setupFakeHelm(t)
	// FAKE_HELM_STATUS_CLUSTERS is unset → helm status exits 1 for every
	// cluster → helmReleaseExists returns false everywhere.
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
		"beta":  "https://beta.example.com",
	})
	args := mustMarshalJSON(t, map[string]interface{}{
		"release_name": "ghost",
		"namespace":    "default",
	})
	_, err := server.handleHelmUninstall(context.Background(), args)
	if err == nil || !strings.Contains(err.Error(), "not found in any cluster") {
		t.Fatalf("err = %v, want 'not found in any cluster'", err)
	}
}

func TestHandleHelmUninstall_MalformedArgs(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})
	_, err := server.handleHelmUninstall(context.Background(), []byte("{not-json"))
	if err == nil || !strings.Contains(err.Error(), "invalid arguments") {
		t.Fatalf("err = %v, want 'invalid arguments' for malformed JSON", err)
	}
}

func TestHandleHelmUninstall_DefaultsNamespaceToDefault(t *testing.T) {
	logFile := setupFakeHelm(t)
	t.Setenv("FAKE_HELM_STATUS_CLUSTERS", "alpha")

	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
	})
	args := mustMarshalJSON(t, map[string]interface{}{
		"release_name": "webapp",
		// namespace deliberately omitted
		"dry_run": false,
	})
	got, err := server.handleHelmUninstall(context.Background(), args)
	if err != nil {
		t.Fatalf("handleHelmUninstall() error = %v", err)
	}
	results := got.(map[string]interface{})["results"].([]HelmResult)
	if results[0].Namespace != "default" {
		t.Fatalf("Namespace = %q, want %q", results[0].Namespace, "default")
	}
	log := readLogFile(t, logFile)
	if !strings.Contains(log, "namespace=default") {
		t.Fatalf("fake helm did not see --namespace default, log:\n%s", log)
	}
}
