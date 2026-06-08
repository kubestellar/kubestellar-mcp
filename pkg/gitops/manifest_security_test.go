package gitops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRepoURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"valid https", "https://github.com/org/repo.git", false},
		{"blocks http scheme", "http://github.com/org/repo.git", true},
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

func TestValidateBranchName(t *testing.T) {
	tests := []struct {
		name    string
		branch  string
		wantErr string
	}{
		{name: "empty uses default", branch: ""},
		{name: "simple branch", branch: "main"},
		{name: "nested branch", branch: "release/v1.2.3"},
		{name: "leading dash", branch: "--upload-pack-touch-pwned", wantErr: "invalid git branch name"},
		{name: "bad characters", branch: "feature;rm -rf /", wantErr: "invalid git branch name"},
		{name: "shell substitution", branch: "feature$(pwd)", wantErr: "invalid git branch name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBranchName(tt.branch)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
