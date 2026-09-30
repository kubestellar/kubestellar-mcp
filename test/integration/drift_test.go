//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/kubestellar/kubestellar-mcp/pkg/gitops"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/drift"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/handlers"
)

// stubManifestReader stands in for the git clone half of detect_drift. Only
// the git round-trip is stubbed: the drift detector itself is the real
// gitops.DriftDetector built from the envtest *rest.Config, so RESTMapper
// discovery, dynamic-client GETs, and the missing/modified classification
// all run against a live apiserver.
type stubManifestReader struct {
	manifests []gitops.Manifest
	err       error
	cleanedUp bool
}

func (r *stubManifestReader) ReadFromGit(_ context.Context, _ gitops.ManifestSource) ([]gitops.Manifest, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.manifests, nil
}

func (r *stubManifestReader) Cleanup() {
	r.cleanedUp = true
}

// newDriftDeps wires handlers.Deps so detect_drift talks to envtest: the
// REST config is the envtest one (so Deps.NewDriftDetector builds the real
// gitops detector against it) and the manifest reader is the stub above.
// DriftDetectorFactory is deliberately left nil so the production
// constructor path is the one under test.
func newDriftDeps(reader handlers.ManifestReader) *handlers.Deps {
	return &handlers.Deps{
		RESTConfigFactory: func(clusterName string) (*rest.Config, error) {
			return testCfg, nil
		},
		ManifestReaderFactory: func() handlers.ManifestReader {
			return reader
		},
	}
}

// driftConfigMapManifest builds a ConfigMap manifest in the shape
// gitops.ManifestReader produces from a YAML document in git.
func driftConfigMapManifest(namespace, name string, data map[string]interface{}) gitops.Manifest {
	return gitops.Manifest{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Metadata: gitops.ManifestMetadata{
			Name:      name,
			Namespace: namespace,
		},
		Data: data,
		Raw: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
			},
			"data": data,
		},
	}
}

// TestDriftDetectDrift exercises pkg/mcp/server/drift's detect_drift MCP
// tool end-to-end against a real kube-apiserver (envtest). Three ConfigMap
// manifests are fed in as if they had been read from git — one that matches
// the cluster exactly, one whose data differs from the seeded object, and
// one that was never created — and the tool's markdown+JSON output is
// asserted for all three classifications.
//
// The value over the package's unit tests (drift_branches_test.go, which
// injects a fake DriftDetector) is that the detector here is the real
// gitops.DriftDetector: RESTMapper discovery has to resolve
// ConfigMap → v1/configmaps against the live discovery document, the
// not-found mapping has to come from a genuine apiserver 404, and the data
// diff has to be computed from the object the apiserver actually returned.
func TestDriftDetectDrift(t *testing.T) {
	ctx := context.Background()

	clientset, err := kubernetes.NewForConfig(testCfg)
	require.NoError(t, err, "kubernetes.NewForConfig")

	const namespace = "mcp-integration-drift"
	const syncedName = "mcp-integration-drift-synced"
	const modifiedName = "mcp-integration-drift-modified"
	const missingName = "mcp-integration-drift-missing"
	const repoURL = "https://github.com/kubestellar/mcp-integration-manifests"

	_, err = clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}, metav1.CreateOptions{})
	require.NoError(t, err, "create namespace")

	_, err = clientset.CoreV1().ConfigMaps(namespace).Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: syncedName, Namespace: namespace},
		Data:       map[string]string{"key": "in-sync"},
	}, metav1.CreateOptions{})
	require.NoError(t, err, "create in-sync configmap")

	_, err = clientset.CoreV1().ConfigMaps(namespace).Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: modifiedName, Namespace: namespace},
		Data:       map[string]string{"key": "changed-in-cluster"},
	}, metav1.CreateOptions{})
	require.NoError(t, err, "create drifted configmap")

	syncedManifest := driftConfigMapManifest(namespace, syncedName, map[string]interface{}{"key": "in-sync"})
	modifiedManifest := driftConfigMapManifest(namespace, modifiedName, map[string]interface{}{"key": "expected-from-git"})
	missingManifest := driftConfigMapManifest(namespace, missingName, map[string]interface{}{"key": "never-applied"})

	reg := handlers.NewRegistry()
	drift.Register(reg)

	handler := reg.Find("detect_drift")
	require.NotNil(t, handler, "detect_drift tool not found in registry after drift.Register")

	tests := []struct {
		name           string
		manifests      []gitops.Manifest
		args           map[string]interface{}
		wantError      bool
		wantContains   []string
		wantNotContain []string
	}{
		{
			name:      "reports missing and modified resources and leaves the synced one out",
			manifests: []gitops.Manifest{syncedManifest, modifiedManifest, missingManifest},
			args:      map[string]interface{}{"repo_url": repoURL, "path": "production/", "branch": "main"},
			wantContains: []string{
				"# GitOps Drift Detection",
				"**Repository:** " + repoURL,
				"**Path:** production/",
				"**Branch:** main",
				"**Manifests Found:** 3",
				"**Drift detected**: 2 resource(s) out of sync",
				"- Missing from cluster: 1",
				"- Modified in cluster: 1",
				missingName,
				"Resource does not exist in cluster",
				modifiedName,
				"data.key",
				`(expected: "expected-from-git")`,
				`"drifted": true`,
			},
			wantNotContain: []string{"### 📝 ConfigMap/" + syncedName},
		},
		{
			name:      "reports no drift when every manifest matches the cluster",
			manifests: []gitops.Manifest{syncedManifest},
			args:      map[string]interface{}{"repo_url": repoURL},
			wantContains: []string{
				"**No drift detected**",
				"**Cluster:** current-context",
				`"drifted": false`,
				`"synced": 1`,
			},
		},
		{
			name:      "namespace filter drops manifests from other namespaces",
			manifests: []gitops.Manifest{syncedManifest, missingManifest},
			args:      map[string]interface{}{"repo_url": repoURL, "namespace": "mcp-integration-drift-other"},
			wantContains: []string{
				"**Manifests Found:** 0",
				"**No drift detected**",
			},
			wantNotContain: []string{missingName},
		},
		{
			name:      "reports an empty git source without contacting the cluster",
			manifests: []gitops.Manifest{},
			args:      map[string]interface{}{"repo_url": repoURL, "path": "empty/"},
			wantContains: []string{
				"No manifests found in " + repoURL,
				"(path: empty/)",
			},
		},
		{
			name:         "rejects a call without repo_url",
			manifests:    []gitops.Manifest{syncedManifest},
			args:         map[string]interface{}{},
			wantError:    true,
			wantContains: []string{"repo_url is required"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &stubManifestReader{manifests: tt.manifests}
			deps := newDriftDeps(reader)

			output, isError := handler(ctx, deps, tt.args)
			require.Equalf(t, tt.wantError, isError, "detect_drift isError mismatch, output: %s", output)

			for _, want := range tt.wantContains {
				require.Containsf(t, output, want, "detect_drift output missing %q", want)
			}
			for _, unwanted := range tt.wantNotContain {
				require.NotContainsf(t, output, unwanted, "detect_drift output unexpectedly mentions %q", unwanted)
			}
		})
	}

	t.Run("the manifest reader is always cleaned up", func(t *testing.T) {
		reader := &stubManifestReader{manifests: []gitops.Manifest{syncedManifest}}
		deps := newDriftDeps(reader)

		_, isError := handler(ctx, deps, map[string]interface{}{"repo_url": repoURL})
		require.False(t, isError, "detect_drift should succeed for an in-sync manifest set")
		require.True(t, reader.cleanedUp, "detect_drift must call ManifestReader.Cleanup")
	})
}
