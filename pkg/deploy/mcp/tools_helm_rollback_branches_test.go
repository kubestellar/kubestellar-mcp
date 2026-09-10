package mcp

import (
	"context"
	"strings"
	"testing"
)

// TestHandleHelmRollback_NoClustersHaveRelease covers the
// "release not found in any cluster" branch of handleHelmRollback
// (tools_helm.go line 769) — reached when no clusters are supplied
// explicitly, discovery finds real clusters, but helmReleaseExists
// returns false for every one of them.
//
// FAKE_HELM_STATUS_CLUSTERS is unset, so the fake helm shim exits 1 for
// every `helm status` invocation, which the real helmReleaseExists
// treats as "release absent". The handler must then fail closed rather
// than proceed to rollback an empty target set (which would look like
// a spurious success at the CI-log level).
func TestHandleHelmRollback_NoClustersHaveRelease(t *testing.T) {
	_ = setupFakeHelm(t)

	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
		"beta":  "https://beta.example.com",
	})
	args := mustMarshalJSON(t, map[string]interface{}{
		"release_name": "ghost",
		"namespace":    "default",
	})

	_, err := server.handleHelmRollback(context.Background(), args)
	if err == nil || !strings.Contains(err.Error(), "not found in any cluster") {
		t.Fatalf("err = %v, want 'not found in any cluster'", err)
	}
}

// TestHandleHelmRollback_DefaultsNamespaceToDefault covers the
// namespace-defaulting branch of handleHelmRollback (tools_helm.go
// lines 732-734). When the caller omits or empties `namespace`, the
// handler must set it to "default" *before* validation, otherwise
// ValidateNamespace would reject empty-string with
// "namespace cannot be empty" and this branch would never fire.
//
// The observable behaviour we lock:
//   1. No "namespace cannot be empty" / "namespace is invalid" error.
//   2. The handler successfully targets a real cluster ("alpha"),
//      demonstrating that "default" was substituted before the
//      helmReleaseExists probe (which requires a valid namespace).
//
// A regression that removed the defaulting block, or moved it after
// ValidateNamespace, would fail case 1.
func TestHandleHelmRollback_DefaultsNamespaceToDefault(t *testing.T) {
	_ = setupFakeHelm(t)
	// Make helmReleaseExists succeed for "alpha" so discovery picks it up.
	t.Setenv("FAKE_HELM_STATUS_CLUSTERS", "alpha")

	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
	})
	args := mustMarshalJSON(t, map[string]interface{}{
		"release_name": "webapp",
		// namespace deliberately omitted — must default to "default".
		"revision": 2,
		"dry_run":  true,
	})

	got, err := server.handleHelmRollback(context.Background(), args)
	if err != nil {
		t.Fatalf("handleHelmRollback() error = %v", err)
	}

	result, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("result is not a map: %#v", got)
	}
	if tc, _ := result["totalClusters"].(int); tc != 1 {
		t.Fatalf("totalClusters = %v, want 1 (alpha should have been targeted after namespace defaulting)", result["totalClusters"])
	}
	if sc, _ := result["successCount"].(int); sc != 1 {
		t.Fatalf("successCount = %v, want 1", result["successCount"])
	}
}
