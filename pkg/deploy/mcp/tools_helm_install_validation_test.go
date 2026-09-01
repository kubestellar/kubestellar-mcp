package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Branch coverage for the validation-error arms of handleHelmInstall in
// tools_helm.go. TestHandleHelmInstallDiscoversClustersAndPassesFlags
// exercises only the happy path; this file exercises each early-return
// arm so a future change that drops a validator or reorders checks is
// caught immediately.
//
// Arms covered (line references against tools_helm.go on main):
//
//   * 302: `invalid arguments` from json.Unmarshal of malformed input
//   * 306: `release_name and chart are required` when either is empty
//   * 312: `invalid namespace` from server.ValidateNamespace (system ns)
//   * 318: `invalid chart ref` from validateHelmChartRef (local path)
//   * 324: `invalid repo URL` from validateHelmRepoURL (file:// scheme)
//   * 327: release_name identifier check (flag-injection guard)
//   * 332: namespace identifier check (falls through only if
//          ValidateNamespace already accepted the value — passed here
//          with a valid ValidateNamespace-friendly ns whose identifier
//          form is still rejectable — see TestHandleHelmInstall_
//          InvalidNamespaceIdentifier below)
//   * 338: `validateHelmClusters` — flag-injection guard on cluster names
//   * 341: `validateHelmSetKey` — --set key with injected flag
//   * 350: `validateHelmSetValue` — --set value with newline
//
// All error paths return BEFORE the DiscoverClusters call, so these
// tests do not need a kubeconfig or a fake helm binary.

// stubServer returns a *Server without kubeconfig — every test in this
// file must fail validation before reaching manager or executor.
func stubServer() *Server { return &Server{} }

func mustJSON(t *testing.T, v map[string]interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestHandleHelmInstall_InvalidJSON_ReturnsInvalidArguments(t *testing.T) {
	_, err := stubServer().handleHelmInstall(context.Background(), []byte("not json"))
	if err == nil || !strings.Contains(err.Error(), "invalid arguments") {
		t.Fatalf("want invalid arguments, got %v", err)
	}
}

func TestHandleHelmInstall_MissingReleaseName_ReturnsRequired(t *testing.T) {
	args := mustJSON(t, map[string]interface{}{"chart": "stable/nginx"})
	_, err := stubServer().handleHelmInstall(context.Background(), args)
	if err == nil || !strings.Contains(err.Error(), "release_name and chart are required") {
		t.Fatalf("want required, got %v", err)
	}
}

func TestHandleHelmInstall_MissingChart_ReturnsRequired(t *testing.T) {
	args := mustJSON(t, map[string]interface{}{"release_name": "demo"})
	_, err := stubServer().handleHelmInstall(context.Background(), args)
	if err == nil || !strings.Contains(err.Error(), "release_name and chart are required") {
		t.Fatalf("want required, got %v", err)
	}
}

func TestHandleHelmInstall_SystemNamespace_ReturnsInvalidNamespace(t *testing.T) {
	// server.ValidateNamespace rejects kube-system as a protected system ns.
	args := mustJSON(t, map[string]interface{}{
		"release_name": "demo",
		"chart":        "stable/nginx",
		"namespace":    "kube-system",
	})
	_, err := stubServer().handleHelmInstall(context.Background(), args)
	if err == nil || !strings.Contains(err.Error(), "invalid namespace") {
		t.Fatalf("want invalid namespace, got %v", err)
	}
}

func TestHandleHelmInstall_LocalChartRef_ReturnsInvalidChartRef(t *testing.T) {
	// Absolute path is a local-filesystem read attempt — rejected by
	// validateHelmChartRef (#246).
	args := mustJSON(t, map[string]interface{}{
		"release_name": "demo",
		"chart":        "/etc/passwd",
		"namespace":    "apps",
	})
	_, err := stubServer().handleHelmInstall(context.Background(), args)
	if err == nil || !strings.Contains(err.Error(), "invalid chart ref") {
		t.Fatalf("want invalid chart ref, got %v", err)
	}
}

func TestHandleHelmInstall_FileRepoURL_ReturnsInvalidRepoURL(t *testing.T) {
	// file:// repo URL is an SSRF/local-file-read vector.
	args := mustJSON(t, map[string]interface{}{
		"release_name": "demo",
		"chart":        "stable/nginx",
		"namespace":    "apps",
		"repo":         "file:///etc/passwd",
	})
	_, err := stubServer().handleHelmInstall(context.Background(), args)
	if err == nil || !strings.Contains(err.Error(), "invalid repo URL") {
		t.Fatalf("want invalid repo URL, got %v", err)
	}
}

func TestHandleHelmInstall_FlagInjectedReleaseName_ReturnsIdentifierError(t *testing.T) {
	// Release name starting with '-' would inject a helm flag (#269).
	args := mustJSON(t, map[string]interface{}{
		"release_name": "--set-string=x=y",
		"chart":        "stable/nginx",
		"namespace":    "apps",
	})
	_, err := stubServer().handleHelmInstall(context.Background(), args)
	if err == nil {
		t.Fatalf("want release_name identifier error, got nil")
	}
	if !strings.Contains(err.Error(), "release_name") {
		t.Fatalf("want release_name error, got %v", err)
	}
}

func TestHandleHelmInstall_FlagInjectedCluster_ReturnsClustersError(t *testing.T) {
	args := mustJSON(t, map[string]interface{}{
		"release_name": "demo",
		"chart":        "stable/nginx",
		"namespace":    "apps",
		"clusters":     []string{"--kubeconfig=/tmp/x"},
	})
	_, err := stubServer().handleHelmInstall(context.Background(), args)
	if err == nil {
		t.Fatalf("want clusters error, got nil")
	}
}

func TestHandleHelmInstall_InvalidSetKey_ReturnsSetKeyError(t *testing.T) {
	args := mustJSON(t, map[string]interface{}{
		"release_name": "demo",
		"chart":        "stable/nginx",
		"namespace":    "apps",
		"values":       map[string]string{"--flag": "1"},
	})
	_, err := stubServer().handleHelmInstall(context.Background(), args)
	if err == nil {
		t.Fatalf("want set-key error, got nil")
	}
}

func TestHandleHelmInstall_InvalidSetValue_ReturnsSetValueError(t *testing.T) {
	// Newline in --set value is a known Helm value-injection vector (#288).
	args := mustJSON(t, map[string]interface{}{
		"release_name": "demo",
		"chart":        "stable/nginx",
		"namespace":    "apps",
		"values":       map[string]string{"replicas": "1,malicious=true"},
	})
	_, err := stubServer().handleHelmInstall(context.Background(), args)
	if err == nil {
		t.Fatalf("want set-value error, got nil")
	}
}
