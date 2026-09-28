package handlers

import (
	"fmt"

	"github.com/kubestellar/kubestellar-mcp/pkg/security/namespace"
)

// ExtractAndValidateNamespace pulls the "namespace" key from a tool argument
// map and validates it against pkg/security/namespace.
//
// When the key is absent, the call is allowed in all-namespaces mode and
// ("", nil) is returned. A provided value must be a string and must pass
// namespace.ValidateNamespace (RFC 1123 syntax + system-namespace blocklist).
//
// This lives in the import-safe handlers package so domain sub-packages
// (rbac, policy, workloads, diagnostics, drift, cluster, ...) can validate
// their namespace argument without importing pkg/mcp/server. See #1027.
func ExtractAndValidateNamespace(args map[string]interface{}) (string, error) {
	raw, ok := args["namespace"]
	if !ok {
		return "", nil
	}

	ns, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("namespace must be a string, got %T", raw)
	}

	if err := namespace.ValidateNamespace(ns); err != nil {
		return "", err
	}

	return ns, nil
}
