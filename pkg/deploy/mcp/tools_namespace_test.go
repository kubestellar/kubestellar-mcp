package mcp

import (
	"strings"
	"testing"

	server "github.com/kubestellar/kubestellar-mcp/pkg/mcp/server"
)

// TestValidateNamespace_Blocked is a smoke-test that verifies the deploy
// server's ValidateNamespace integration rejects a blocked system namespace.
func TestValidateNamespace_Blocked(t *testing.T) {
	err := server.ValidateNamespace("kube-system")
	if err == nil {
		t.Fatal("expected error for blocked namespace kube-system, got nil")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("error message %q does not contain %q", err.Error(), "not allowed")
	}
}

// TestValidateNamespace_Allowed is a smoke-test that verifies the deploy
// server's ValidateNamespace integration allows a safe user namespace.
func TestValidateNamespace_Allowed(t *testing.T) {
	err := server.ValidateNamespace("my-app")
	if err != nil {
		t.Fatalf("expected no error for allowed namespace my-app, got %v", err)
	}
}
