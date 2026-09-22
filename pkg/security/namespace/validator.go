// Package namespace owns the AI-facing Kubernetes-namespace safety predicate
// shared by every MCP server in this repo. Extracted from pkg/mcp/server so
// pkg/deploy/mcp (a peer feature package) no longer has to reach into another
// server package for a single security utility. See
// kubestellar/kubestellar-mcp#964.
package namespace

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
