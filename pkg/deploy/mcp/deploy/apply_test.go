package deploy

import (
	"context"
	"errors"
	"testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/gitops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestApplyManifest_DryRunDefaultsNamespace(t *testing.T) {
	fd := newFakeDeps()
	manifest := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n"

	results, err := ApplyManifest(context.Background(), fd.deps(), nil, "alpha", manifest, true)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "would-apply", results[0].Status)
	assert.Contains(t, results[0].Message, "namespace default")
}

func TestApplyManifest_DryRunSkipsNilDocuments(t *testing.T) {
	fd := newFakeDeps()
	manifest := "---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n---\n---\n"

	results, err := ApplyManifest(context.Background(), fd.deps(), nil, "alpha", manifest, true)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Contains(t, results[0].Resource, "ConfigMap/demo")
}

func TestApplyManifest_DryRunReportsInvalidNamespace(t *testing.T) {
	fd := newFakeDeps()
	manifest := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n  namespace: kube-system\n"

	results, err := ApplyManifest(context.Background(), fd.deps(), nil, "alpha", manifest, true)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "failed", results[0].Status)
	assert.Contains(t, results[0].Message, "invalid namespace in manifest")
}

func TestApplyManifest_DryRunDecodeError(t *testing.T) {
	fd := newFakeDeps()
	_, err := ApplyManifest(context.Background(), fd.deps(), nil, "alpha", "[", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode manifest")
}

func TestApplyManifest_NonDryRunDecodeError(t *testing.T) {
	fd := newFakeDeps()
	fd.manifestReader = &fakeManifestReader{err: errors.New("decode boom")}
	_, err := ApplyManifest(context.Background(), fd.deps(), nil, "alpha", "irrelevant", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode manifest")
}

func TestApplyManifest_NonDryRunGetConfigError(t *testing.T) {
	fd := newFakeDeps()
	fd.manifestReader = &fakeManifestReader{}
	fd.configErrs["does-not-exist"] = errors.New("no such cluster")
	_, err := ApplyManifest(context.Background(), fd.deps(), nil, "does-not-exist", "irrelevant", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get config for cluster does-not-exist")
}

func TestApplyManifest_NonDryRunSyncerFactoryError(t *testing.T) {
	fd := newFakeDeps()
	fd.manifestReader = &fakeManifestReader{}
	factoryErr := errors.New("factory boom")
	fd.manifestSyncer = func(*rest.Config) (ManifestSyncer, error) { return nil, factoryErr }
	_, err := ApplyManifest(context.Background(), fd.deps(), nil, "alpha", "irrelevant", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create manifest syncer")
	assert.ErrorIs(t, err, factoryErr)
}

type erroringManifestSyncer struct{ err error }

func (e *erroringManifestSyncer) Sync(context.Context, []gitops.Manifest, string, gitops.SyncOptions) (*gitops.SyncSummary, error) {
	return nil, e.err
}

func TestApplyManifest_NonDryRunSyncError(t *testing.T) {
	fd := newFakeDeps()
	fd.manifestReader = &fakeManifestReader{}
	syncErr := errors.New("sync boom")
	fd.manifestSyncer = func(*rest.Config) (ManifestSyncer, error) {
		return &erroringManifestSyncer{err: syncErr}, nil
	}
	_, err := ApplyManifest(context.Background(), fd.deps(), nil, "alpha", "irrelevant", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to apply manifest")
	assert.ErrorIs(t, err, syncErr)
}

type capturingManifestSyncer struct{ kinds []string }

func (s *capturingManifestSyncer) Sync(_ context.Context, manifests []gitops.Manifest, clusterName string, _ gitops.SyncOptions) (*gitops.SyncSummary, error) {
	results := make([]gitops.SyncResult, 0, len(manifests))
	for _, manifest := range manifests {
		s.kinds = append(s.kinds, manifest.Kind)
		results = append(results, gitops.SyncResult{
			Cluster: clusterName, Kind: manifest.Kind, Name: manifest.Metadata.Name,
			Namespace: manifest.Metadata.Namespace, Action: gitops.SyncActionCreated,
		})
	}
	return &gitops.SyncSummary{Cluster: clusterName, Created: len(results), Results: results}, nil
}

func TestApplyManifest_NonDryRunSuccess(t *testing.T) {
	fd := newFakeDeps()
	fd.manifestReader = &fakeManifestReader{manifests: []gitops.Manifest{
		{Kind: "ConfigMap", Metadata: gitops.ManifestMetadata{Name: "demo"}},
	}}
	syncer := &capturingManifestSyncer{}
	fd.manifestSyncer = func(*rest.Config) (ManifestSyncer, error) { return syncer, nil }

	results, err := ApplyManifest(context.Background(), fd.deps(), nil, "alpha", "irrelevant", false)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "created", results[0].Status)
	assert.Equal(t, []string{"ConfigMap"}, syncer.kinds)
}
