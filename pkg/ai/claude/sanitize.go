package claude

import (
	"fmt"
	"regexp"
	"strings"
)

var validClusterNamePattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
var validK8sNamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)
var validK8sNamespacePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

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

// ValidateK8sNamespace validates a Kubernetes namespace name following RFC 1123
// DNS label rules: lowercase alphanumeric with hyphens, must start and end with
// alphanumeric, max 63 characters.
func ValidateK8sNamespace(namespace string) error {
	if namespace == "" {
		return nil // empty namespace is valid (means all namespaces)
	}
	if len(namespace) > 63 {
		return fmt.Errorf("namespace must be 63 characters or less")
	}
	if !validK8sNamespacePattern.MatchString(namespace) {
		return fmt.Errorf("namespace must match pattern ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$")
	}
	return nil
}
