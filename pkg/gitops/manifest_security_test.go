package gitops

import (
	"os"
	"path/filepath"
	"strings"
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
		// SSRF IP-literal tests (#276)
		{"blocks cloud metadata IP", "https://169.254.169.254/latest/meta-data/", true},
		{"blocks private 10.x", "https://10.0.0.1/repo.git", true},
		{"blocks private 172.16.x", "https://172.16.0.1/repo.git", true},
		{"blocks private 192.168.x", "https://192.168.1.1/repo.git", true},
		{"blocks loopback", "https://127.0.0.1/repo.git", true},
		{"blocks CGNAT", "https://100.64.0.1/repo.git", true},
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

// TestReadFromPathSkipsSymlinks verifies that a `.yaml` symlink pointing
// outside the cloned tree is not dereferenced by ReadFromPath. Regression
// for issue #945: without this guard an attacker-controlled git repo could
// commit `leak.yaml -> /var/run/secrets/.../token` (or any host file that
// parses as YAML) and have its contents echoed back through drift-detect /
// sync responses.
func TestReadFromPathSkipsSymlinks(t *testing.T) {
	if _, err := os.Lstat("/dev/null"); err != nil {
		t.Skipf("no filesystem symlink support: %v", err)
	}
	tempDir := t.TempDir()

	// Sensitive file that lives OUTSIDE the "repo" clone directory.
	// Use valid YAML so that if the symlink were followed the parse would
	// succeed and the content would flow into a returned Manifest — the
	// exact exfiltration path this test guards against.
	secretDir := t.TempDir()
	secretFile := filepath.Join(secretDir, "host-secret.yaml")
	const secretMarker = "SUPER_SECRET_TOKEN_VALUE_a83f2b1c9d"
	require.NoError(t, os.WriteFile(secretFile,
		[]byte("apiVersion: v1\nkind: Secret\nmetadata:\n  name: leaked\nstringData:\n  token: "+secretMarker+"\n"),
		0o600))

	// A benign regular manifest inside the "repo".
	benign := filepath.Join(tempDir, "ok.yaml")
	require.NoError(t, os.WriteFile(benign,
		[]byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: benign\n"),
		0o600))

	// The malicious symlink inside the "repo" pointing at the host secret.
	leak := filepath.Join(tempDir, "leak.yaml")
	require.NoError(t, os.Symlink(secretFile, leak))

	r := NewManifestReader()
	manifests, err := r.ReadFromPath(tempDir)
	require.NoError(t, err)

	// Only the benign manifest should have been read; the symlink target
	// content must not have leaked into any Manifest.Raw.
	require.Len(t, manifests, 1)
	assert.Equal(t, "ConfigMap", manifests[0].Kind)
	for _, m := range manifests {
		for k, v := range m.Raw {
			if s, ok := v.(string); ok {
				assert.NotContains(t, s, secretMarker,
					"symlink target content leaked into Manifest.Raw[%q]", k)
			}
		}
	}
	// And the marker must not appear anywhere in the flattened dump.
	assert.NotContains(t, flattenRaw(manifests), secretMarker)
}

// flattenRaw is a test helper: a compact recursive stringifier so the
// assertion above catches leaks in nested maps (e.g. stringData) too.
func flattenRaw(ms []Manifest) string {
	var sb strings.Builder
	var walk func(v interface{})
	walk = func(v interface{}) {
		switch t := v.(type) {
		case map[string]interface{}:
			for k, vv := range t {
				sb.WriteString(k)
				sb.WriteString("=")
				walk(vv)
				sb.WriteString(",")
			}
		case []interface{}:
			for _, vv := range t {
				walk(vv)
				sb.WriteString(",")
			}
		default:
			sb.WriteString("<val>")
		}
	}
	for _, m := range ms {
		walk(m.Raw)
	}
	return sb.String()
}
