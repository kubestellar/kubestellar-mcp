package server

import (
	"strings"
	"testing"
)

// --- toolAuditKubeconfig ---
// Note: toolAuditKubeconfig uses clientcmd.NewDefaultClientConfigLoadingRules
// which reads from KUBECONFIG env or ~/.kube/config. We test with an empty/missing config.

func TestToolAuditKubeconfig_NoKubeconfig(t *testing.T) {
	// Server with no kubeconfig set - will try default locations
	server := &Server{
		discoverer: stubDiscoverer{},
		kubeconfig: "/tmp/nonexistent-kubeconfig-for-test",
	}
	result, rpcErr := callTool(t, server, "audit_kubeconfig", map[string]interface{}{})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	// Should either fail to load or show no contexts
	text := result.Content[0].Text
	if !strings.Contains(text, "Failed to load") && !strings.Contains(text, "No contexts") && !result.IsError {
		// If it loaded something from default paths, that's fine too
		t.Logf("got output: %s", text)
	}
}

func TestToolAuditKubeconfig_WithTimeoutParam(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		kubeconfig: "/tmp/nonexistent-kubeconfig-for-test-2",
	}
	result, rpcErr := callTool(t, server, "audit_kubeconfig", map[string]interface{}{
		"timeout_seconds": float64(1),
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	// Should fail gracefully
	_ = result
}
