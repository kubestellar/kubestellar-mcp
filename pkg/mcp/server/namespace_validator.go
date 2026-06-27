package server

import (
	"fmt"
	"strings"
)

// blockedExact is a set of system namespace names that are not allowed for
// AI-driven operations regardless of the action being performed.
// Using a blocklist (rather than an allowlist) ensures that user-defined
// namespaces continue to work without an explicit registration step.
var blockedExact = map[string]bool{
	"kube-system":       true,
	"kube-public":       true,
	"kube-node-lease":   true,
	"gatekeeper-system": true,
	"openshift":         true,
}

// ValidateNamespace checks whether the supplied namespace is allowed for
// AI-driven operations. An empty string (all-namespaces mode) is always
// accepted. The blocklist match is exact/string-based (map lookup + prefix
// check are case-sensitive). Kubernetes namespace names are constrained to
// DNS-1123 labels (lowercase alphanumeric), so case collisions are unlikely.
func ValidateNamespace(ns string) error {
	if ns == "" {
		return nil
	}
	if blockedExact[ns] {
		return fmt.Errorf("access to system namespace %q is not allowed", ns)
	}
	if strings.HasPrefix(ns, "openshift-") {
		return fmt.Errorf("access to system namespace %q is not allowed", ns)
	}
	return nil
}

// extractAndValidateNamespace pulls the "namespace" key from a tool argument
// map and validates it. When the key is absent or the value is an empty
// string, the call is allowed (all-namespaces mode) and ("", nil) is returned.
// A non-string value is rejected with an error, preventing type-coercion
// bypass of the namespace blocklist.
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
