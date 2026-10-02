// Package sanitize provides generic Kubernetes-name validators and a
// prompt-injection-safe string sanitizer. These utilities previously lived in
// pkg/ai/claude but have no vendor-specific dependency; relocating them lets
// non-AI feature packages (e.g. pkg/deploy/mcp/app) call them without pulling
// in pkg/ai/claude. See kubestellar/kubestellar-mcp architect finding.
package sanitize

import (
	"fmt"
	"regexp"
	"strings"
)

var validClusterNamePattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
var validK8sNamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)

// SanitizeForPrompt sanitizes user-controlled strings before injecting them
// into AI prompts to prevent prompt injection attacks. It removes newlines,
// control characters, and truncates long strings.
func SanitizeForPrompt(s string) string {
	// Replace newlines and carriage returns with spaces
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\t", " ")

	// Remove other control characters (ASCII 0-31 except space)
	var sb strings.Builder
	for _, r := range s {
		if r >= 32 || r == ' ' {
			sb.WriteRune(r)
		}
	}
	s = sb.String()

	// Collapse multiple spaces
	s = strings.Join(strings.Fields(s), " ")

	// Truncate to prevent token stuffing (200 chars is reasonable for cluster/namespace names)
	if len(s) > 200 {
		s = s[:200] + "..."
	}

	return s
}

// ValidateClusterName checks if a cluster name matches the expected pattern.
// Returns the sanitized name if valid, or a safe placeholder if invalid.
func ValidateClusterName(name string) string {
	if !validClusterNamePattern.MatchString(name) {
		return "[invalid-cluster-name]"
	}
	return SanitizeForPrompt(name)
}

// ValidateK8sName validates a Kubernetes resource name (pod, deployment, etc.)
// following RFC 1123 DNS label rules: lowercase alphanumeric with hyphens and dots,
// must start and end with alphanumeric, max 63 characters.
func ValidateK8sName(name string) error {
	if name == "" {
		return fmt.Errorf("name cannot be empty")
	}
	if len(name) > 63 {
		return fmt.Errorf("name must be 63 characters or less")
	}
	if !validK8sNamePattern.MatchString(name) {
		return fmt.Errorf("name must match pattern ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$")
	}
	return nil
}
