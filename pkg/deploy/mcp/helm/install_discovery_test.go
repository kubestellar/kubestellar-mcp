package helm

import (
	"context"
	"strings"
	"testing"
)

// Branch coverage for the remaining arms of parseHelmInstallArgs that
// the existing tools_helm_install_validation_test.go does not reach:
//
//   * `params.Namespace = "default"` — when the caller omits namespace,
//     it must be defaulted before validation (a caller that omitted
//     namespace and a caller that passed `"default"` must be
//     indistinguishable downstream).
//   * `no clusters available` — when the caller passes no explicit
//     `clusters` and DiscoverClusters returns an empty list, the call
//     must fail with a specific error, not proceed to fan-out over zero
//     clusters (which would silently return a zero-cluster success).
//
// Both are pure branches of parseHelmInstallArgs — no helm subprocess
// is invoked, so no fake helm binary is required.

// TestParseHelmInstallArgs_NoClusters_ReturnsNoClustersAvailable exercises
// the empty-DiscoverClusters branch: a kubeconfig with zero contexts must
// produce "no clusters available", not a zero-length successful response.
func TestParseHelmInstallArgs_NoClusters_ReturnsNoClustersAvailable(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	args := mustMarshalJSON(t, map[string]interface{}{
		"release_name": "demo",
		"chart":        "stable/nginx",
		"namespace":    "demo-ns",
	})
	_, err := server.handleHelmInstall(context.Background(), args)
	if err == nil || !strings.Contains(err.Error(), "no clusters available") {
		t.Fatalf("want 'no clusters available', got %v", err)
	}
}

// TestParseHelmInstallArgs_OmittedNamespace_DefaultsToDefault exercises
// the `if params.Namespace == "" { params.Namespace = "default" }`
// branch. We rely on the fact that "default" is a valid namespace for
// validateHelmInstallParams, so the request advances all the way to the
// DiscoverClusters step; with an empty kubeconfig it then fails with
// "no clusters available", proving that the defaulting branch ran (an
// empty namespace would have been rejected by ValidateNamespace with an
// "invalid namespace" error much earlier).
func TestParseHelmInstallArgs_OmittedNamespace_DefaultsToDefault(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	// No "namespace" key — must be defaulted, not rejected.
	args := mustMarshalJSON(t, map[string]interface{}{
		"release_name": "demo",
		"chart":        "stable/nginx",
	})
	_, err := server.handleHelmInstall(context.Background(), args)
	if err == nil {
		t.Fatalf("want error from empty-cluster kubeconfig, got nil")
	}
	if strings.Contains(err.Error(), "invalid namespace") {
		t.Fatalf("namespace was not defaulted before validation: %v", err)
	}
	if !strings.Contains(err.Error(), "no clusters available") {
		t.Fatalf("expected to reach cluster-discovery arm, got %v", err)
	}
}

// TestParseHelmInstallArgs_ExplicitClustersSkipsDiscovery guards the
// short-circuit where an explicit `clusters` list bypasses
// DiscoverClusters entirely. If a future refactor moved discovery
// unconditionally in front of the check, this test would start to fail
// (or start reaching the helm subprocess) — either signals the bug.
// We assert the request advances past parseHelmInstallArgs by observing
// that the resulting error does NOT come from parseHelmInstallArgs's
// early-return arms (no "invalid arguments", no "required", no "no
// clusters available") — it must instead come from a downstream stage.
func TestParseHelmInstallArgs_ExplicitClustersSkipsDiscovery(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	args := mustMarshalJSON(t, map[string]interface{}{
		"release_name": "demo",
		"chart":        "stable/nginx",
		"namespace":    "demo-ns",
		"clusters":     []string{"explicit-a", "explicit-b"},
		"dry_run":      true,
	})
	// We do not assert success — the fake kubeconfig cannot really run
	// helm — but we DO assert we got past parseHelmInstallArgs.
	// handleHelmInstall returns (result, nil) on the fan-out path and
	// records failures per-cluster in results; either shape is fine as
	// long as parseHelmInstallArgs did not short-circuit with one of
	// its early-return errors.
	result, err := server.handleHelmInstall(context.Background(), args)
	if err != nil {
		earlyReturnMarkers := []string{
			"invalid arguments",
			"release_name and chart are required",
			"no clusters available",
		}
		for _, m := range earlyReturnMarkers {
			if strings.Contains(err.Error(), m) {
				t.Fatalf("explicit clusters short-circuited into parseHelmInstallArgs early-return %q: %v", m, err)
			}
		}
		return
	}
	if result == nil {
		t.Fatalf("expected non-nil result from fan-out over explicit clusters")
	}
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{} result, got %T", result)
	}
	if got := m["totalClusters"]; got != 2 {
		t.Fatalf("expected totalClusters=2 (explicit list preserved), got %v", got)
	}
}
