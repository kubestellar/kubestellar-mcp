package claude

import (
	"regexp"
	"strings"
)

var validClusterNamePattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

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
