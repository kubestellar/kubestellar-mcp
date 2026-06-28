package server

import (
	"fmt"
	"regexp"
	"strings"
)

// blockedExact is a set of system namespace names that are not allowed for
// AI-driven operations regardless of the action being performed.
var blockedExact = map[string]bool{
	"kube-system":       true,
	"kube-public":       true,
	"kube-node-lease":   true,
	"gatekeeper-system": true,
	"openshift":         true,
}

var k8sNamespaceRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// ValidateNamespace checks whether the supplied namespace is allowed for
// AI-driven operations. Namespace values must be valid RFC 1123 DNS labels
// before the system namespace blocklist is applied.
func ValidateNamespace(ns string) error {
	if ns == "" {
		return fmt.Errorf("namespace cannot be empty")
	}
	if len(ns) > 63 {
		return fmt.Errorf("namespace exceeds maximum length of 63 characters")
	}
	if !k8sNamespaceRe.MatchString(ns) {
		return fmt.Errorf("namespace %q is invalid: must be lowercase alphanumeric and hyphens only", ns)
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
