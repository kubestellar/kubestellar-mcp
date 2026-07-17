package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestHandleNamespaceValidationRemaining closes the last block of
// server.ValidateNamespace call-sites in pkg/deploy/mcp/ that had no
// direct rejection-branch coverage — extending the pattern from #525
// (labels), #526 (helm), and #527 (app):
//
//   - tools_kubectl.go: handleDeleteResource
//   - tools_deploy.go:  handleScaleApp, handlePatchApp
//   - tools_gitops.go:  handleSyncFromGit
//
// Every case asserts the shared "invalid namespace" wrapper so
// refactors of the wrapping message still fail loud.
func TestHandleNamespaceValidationRemaining(t *testing.T) {
	s := &Server{}

	cases := []struct {
		name string
		call func() error
		want string
	}{
		// handleDeleteResource — namespace check runs after
		// isSensitiveKind, so use a benign kind (configmap).
		{
			name: "delete blocked system namespace",
			call: func() error {
				_, err := s.handleDeleteResource(context.Background(), json.RawMessage(`{"kind":"configmap","name":"demo","namespace":"kube-system"}`))
				return err
			},
			want: "invalid namespace",
		},
		{
			name: "delete openshift-prefixed namespace",
			call: func() error {
				_, err := s.handleDeleteResource(context.Background(), json.RawMessage(`{"kind":"configmap","name":"demo","namespace":"openshift-monitoring"}`))
				return err
			},
			want: "invalid namespace",
		},
		{
			name: "delete invalid namespace format",
			call: func() error {
				_, err := s.handleDeleteResource(context.Background(), json.RawMessage(`{"kind":"configmap","name":"demo","namespace":"Invalid_NS"}`))
				return err
			},
			want: "invalid namespace",
		},

		// handleScaleApp — namespace check is the first non-parse
		// gate, so simple JSON is sufficient to trigger it.
		{
			name: "scale blocked system namespace",
			call: func() error {
				_, err := s.handleScaleApp(context.Background(), json.RawMessage(`{"app":"demo","namespace":"kube-public","replicas":3}`))
				return err
			},
			want: "invalid namespace",
		},
		{
			name: "scale gatekeeper-system namespace",
			call: func() error {
				_, err := s.handleScaleApp(context.Background(), json.RawMessage(`{"app":"demo","namespace":"gatekeeper-system","replicas":1}`))
				return err
			},
			want: "invalid namespace",
		},

		// handlePatchApp — same pattern as handleScaleApp.
		{
			name: "patch blocked system namespace",
			call: func() error {
				_, err := s.handlePatchApp(context.Background(), json.RawMessage(`{"app":"demo","namespace":"kube-node-lease","patch":"{}"}`))
				return err
			},
			want: "invalid namespace",
		},
		{
			name: "patch openshift-prefixed namespace",
			call: func() error {
				_, err := s.handlePatchApp(context.Background(), json.RawMessage(`{"app":"demo","namespace":"openshift-logging","patch":"{}"}`))
				return err
			},
			want: "invalid namespace",
		},

		// handleSyncFromGit — namespace override is validated after
		// the required "repo" field, so both must be present to
		// reach the check.
		{
			name: "sync blocked system namespace override",
			call: func() error {
				_, err := s.handleSyncFromGit(context.Background(), json.RawMessage(`{"repo":"https://example.com/x.git","namespace":"kube-system"}`))
				return err
			},
			want: "invalid namespace",
		},
		{
			name: "sync invalid namespace format",
			call: func() error {
				_, err := s.handleSyncFromGit(context.Background(), json.RawMessage(`{"repo":"https://example.com/x.git","namespace":"Invalid_NS"}`))
				return err
			},
			want: "invalid namespace",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

// TestValidateManifestDocs covers the shared helper used by the
// kustomize handlers before piping built output to kubectl apply/delete
// (tools_kustomize.go:169 and :281). The helper enforces two rules per
// yaml document: (1) sensitive-kind blocklist, (2) system-namespace
// blocklist. Previously it had no direct tests.
func TestValidateManifestDocs(t *testing.T) {
	cases := []struct {
		name     string
		manifest string
		want     string // "" means expect no error
	}{
		{
			name:     "empty manifest is allowed",
			manifest: "",
			want:     "",
		},
		{
			name:     "whitespace-only doc is skipped",
			manifest: "---\n   \n---\n",
			want:     "",
		},
		{
			name:     "clean configmap in default namespace passes",
			manifest: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n  namespace: default\n",
			want:     "",
		},
		{
			name:     "configmap with no namespace passes",
			manifest: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n",
			want:     "",
		},
		{
			name:     "configmap in kube-system is rejected",
			manifest: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n  namespace: kube-system\n",
			want:     "invalid namespace in manifest",
		},
		{
			name:     "configmap in openshift-monitoring is rejected",
			manifest: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n  namespace: openshift-monitoring\n",
			want:     "invalid namespace in manifest",
		},
		{
			name:     "second doc with kube-public is rejected",
			manifest: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: ok\n  namespace: default\n---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: bad\n  namespace: kube-public\n",
			want:     "invalid namespace in manifest",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateManifestDocs(tc.manifest)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

// TestValidateManifestDocsSensitiveKinds asserts that the helper also
// rejects sensitive resource kinds regardless of namespace — the same
// check that handleKubectlApply / handleDeleteResource perform inline.
func TestValidateManifestDocsSensitiveKinds(t *testing.T) {
	// A ClusterRoleBinding is a canonical sensitive kind (see
	// isSensitiveKind / manifestSensitiveKind).
	manifest := "apiVersion: rbac.authorization.k8s.io/v1\nkind: ClusterRoleBinding\nmetadata:\n  name: hostile\n"
	err := validateManifestDocs(manifest)
	if err == nil {
		t.Fatalf("expected error for sensitive kind, got nil")
	}
}
