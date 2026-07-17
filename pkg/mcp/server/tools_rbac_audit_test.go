package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeKubeconfig writes body to a temp file under t.TempDir() and returns the
// path.  Failing to write is a test failure.
func writeKubeconfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	return path
}

// --- toolAuditKubeconfig extended tests (was 6.5% coverage) ---

func TestToolAuditKubeconfig_MalformedFile(t *testing.T) {
	// Non-YAML nonsense triggers the clientcmd loader's parse error path.
	path := writeKubeconfig(t, "this is: not: valid: yaml: [unbalanced\n")
	server := &Server{
		discoverer: stubDiscoverer{},
		kubeconfig: path,
	}
	result, rpcErr := callTool(t, server, "audit_kubeconfig", map[string]interface{}{})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true for malformed kubeconfig, body: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "Failed to load kubeconfig") {
		t.Fatalf("expected 'Failed to load kubeconfig', got: %s", result.Content[0].Text)
	}
}

func TestToolAuditKubeconfig_NoContexts(t *testing.T) {
	// A valid but empty kubeconfig — zero contexts branch.
	path := writeKubeconfig(t, "apiVersion: v1\nkind: Config\ncontexts: []\nclusters: []\nusers: []\ncurrent-context: \"\"\n")
	server := &Server{
		discoverer: stubDiscoverer{},
		kubeconfig: path,
	}
	result, rpcErr := callTool(t, server, "audit_kubeconfig", map[string]interface{}{})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !strings.Contains(result.Content[0].Text, "No contexts found in kubeconfig") {
		t.Fatalf("expected 'No contexts found', got: %s", result.Content[0].Text)
	}
}

func TestToolAuditKubeconfig_InaccessibleClustersWithDuplicatesAndOrphans(t *testing.T) {
	// Two contexts pointing to the SAME unresolvable server (triggers the
	// consolidation-suggestions branch), plus one context pointing to a
	// different unresolvable server so we get orphaned cluster/user cleanup.
	//
	// The chosen server hostnames use the reserved .invalid TLD (RFC 6761)
	// which is guaranteed never to resolve, producing a stable
	// "no such host" DNS error on every platform.
	body := `apiVersion: v1
kind: Config
current-context: ctx-a
clusters:
- name: cluster-shared
  cluster:
    server: https://shared.audit-test.invalid:6443
    insecure-skip-tls-verify: true
- name: cluster-solo
  cluster:
    server: https://solo.audit-test.invalid:6443
    insecure-skip-tls-verify: true
users:
- name: user-shared
  user:
    token: abc
- name: user-solo
  user:
    token: def
contexts:
- name: ctx-a
  context:
    cluster: cluster-shared
    user: user-shared
- name: ctx-b
  context:
    cluster: cluster-shared
    user: user-shared
- name: ctx-c
  context:
    cluster: cluster-solo
    user: user-solo
`
	path := writeKubeconfig(t, body)
	server := &Server{
		discoverer: stubDiscoverer{},
		kubeconfig: path,
	}
	result, rpcErr := callTool(t, server, "audit_kubeconfig", map[string]interface{}{
		"timeout_seconds": float64(1),
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	text := result.Content[0].Text

	// Summary counts.
	assertContains(t, text, "**Total contexts:** 3")
	assertContains(t, text, "**Inaccessible:** 3")

	// Inaccessible-clusters section rendered.
	assertContains(t, text, "## Inaccessible Clusters")
	assertContains(t, text, "**(current)**")
	// Each context enumerated by name.
	assertContains(t, text, "- **ctx-a**")
	assertContains(t, text, "- **ctx-b**")
	assertContains(t, text, "- **ctx-c**")
	// Error message is simplified — either DNS or timeout depending on resolver behavior.
	if !strings.Contains(text, "DNS resolution failed") &&
		!strings.Contains(text, "Connection timeout") &&
		!strings.Contains(text, "Connection refused") {
		t.Fatalf("expected a simplified network-error string, got:\n%s", text)
	}

	// Duplicate detection: ctx-a and ctx-b share cluster-shared/server URL.
	assertContains(t, text, "## Consolidation Suggestions")
	assertContains(t, text, "shared.audit-test.invalid")

	// Cleanup section for inaccessible contexts.
	assertContains(t, text, "## Delete Inaccessible Contexts")
	assertContains(t, text, "kubectl config delete-context ctx-a")
	assertContains(t, text, "kubectl config delete-context ctx-b")
	assertContains(t, text, "kubectl config delete-context ctx-c")

	// Orphaned cluster/user cleanup: BOTH clusters and users are only used
	// by inaccessible contexts so they must all appear.
	assertContains(t, text, "Also remove orphaned clusters and users")
	assertContains(t, text, "kubectl config delete-cluster cluster-shared")
	assertContains(t, text, "kubectl config delete-cluster cluster-solo")
	assertContains(t, text, "kubectl config delete-user user-shared")
	assertContains(t, text, "kubectl config delete-user user-solo")

	// The "All Good!" summary must NOT appear when there are duplicates
	// AND inaccessible contexts.
	if strings.Contains(text, "## All Good!") {
		t.Fatalf("did not expect 'All Good!' section in report, got:\n%s", text)
	}
}

func TestToolAuditKubeconfig_AccessibleClusterAllGood(t *testing.T) {
	// Stand up a fake API server serving the discovery /version endpoint so
	// clientset.Discovery().ServerVersion() succeeds and we exercise the
	// "Accessible" render path + the "All Good!" summary branch.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"major":"1","minor":"29","gitVersion":"v1.29.3","gitCommit":"abc","gitTreeState":"clean","buildDate":"2026-01-01T00:00:00Z","goVersion":"go1.22","compiler":"gc","platform":"linux/amd64"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	body := fmt.Sprintf(`apiVersion: v1
kind: Config
current-context: ctx-live
clusters:
- name: cluster-live
  cluster:
    server: %s
    insecure-skip-tls-verify: true
users:
- name: user-live
  user:
    token: abc
contexts:
- name: ctx-live
  context:
    cluster: cluster-live
    user: user-live
`, ts.URL)
	path := writeKubeconfig(t, body)
	server := &Server{
		discoverer: stubDiscoverer{},
		kubeconfig: path,
	}
	result, rpcErr := callTool(t, server, "audit_kubeconfig", map[string]interface{}{
		"timeout_seconds": float64(5),
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	text := result.Content[0].Text

	assertContains(t, text, "**Total contexts:** 1")
	assertContains(t, text, "**Accessible:** 1")
	assertContains(t, text, "**Inaccessible:** 0")
	assertContains(t, text, "## Accessible Clusters")
	assertContains(t, text, "- **ctx-live** **(current)**")
	assertContains(t, text, "Version: vv1.29.3")
	assertContains(t, text, "## All Good!")
	// No cleanup section when all accessible + no duplicates.
	if strings.Contains(text, "## Delete Inaccessible Contexts") {
		t.Fatalf("did not expect delete section, got:\n%s", text)
	}
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("missing %q in output:\n%s", needle, haystack)
	}
}
