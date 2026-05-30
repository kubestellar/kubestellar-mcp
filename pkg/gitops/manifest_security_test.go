package gitops

import (
	"testing"
)

func TestValidateRepoURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"valid https", "https://github.com/org/repo.git", false},
		{"valid http", "http://github.com/org/repo.git", false},
		{"blocks file scheme", "file:///etc/kubernetes/pki/ca.key", true},
		{"blocks ssh scheme", "ssh://internal:22/repo", true},
		{"blocks git scheme", "git://host/repo", true},
		{"blocks no scheme", "/etc/passwd", true},
		{"blocks empty", "", true},
		{"blocks scheme-only no host", "https://", true},
		{"blocks scp-like (no scheme)", "git@github.com:org/repo.git", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRepoURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRepoURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}
