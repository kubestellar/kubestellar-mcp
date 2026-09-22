package server

import (
	"fmt"

	"github.com/kubestellar/kubestellar-mcp/pkg/security/namespace"
)

// ValidateNamespace delegates to pkg/security/namespace, which is the single
// source of truth for the AI-facing namespace safety predicate (RFC 1123
// syntax + system-namespace blocklist). Kept here as a thin re-export so
// existing server-side call sites and tests keep compiling; new code should
// import pkg/security/namespace directly. See
// kubestellar/kubestellar-mcp#964.
func ValidateNamespace(ns string) error {
	return namespace.ValidateNamespace(ns)
}

// extractAndValidateNamespace pulls the "namespace" key from a tool argument
// map and validates it. When the key is absent, the call is allowed in
// all-namespaces mode and ("", nil) is returned. A provided namespace must be
// a non-empty string and pass ValidateNamespace.
func extractAndValidateNamespace(args map[string]interface{}) (string, error) {
	raw, ok := args["namespace"]
	if !ok {
		return "", nil
	}

	ns, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("namespace must be a string, got %T", raw)
	}

	if err := ValidateNamespace(ns); err != nil {
		return "", err
	}

	return ns, nil
}
