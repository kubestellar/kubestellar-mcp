package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/gitops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// These tests cover the per-cluster sync body inside handleSyncFromGit's
// runGitOpsClusterTasks closure (tools_gitops.go) now that it routes through
// the s.getManifestSyncer factory instead of calling gitops.NewSyncer
// directly. Because the factory is injectable, we can exercise:
//   - the syncer-construction error branch ("Failed to create syncer")
//   - the syncer.Sync error branch ("Failed to sync")
//   - the success branch (summary appended verbatim to result.Summaries)
// without talking to a real API server.

// stubManifestSyncer is a fully-controllable manifestSyncer test double.
type stubManifestSyncer struct {
	summary *gitops.SyncSummary
	err     error
}

func (s *stubManifestSyncer) Sync(_ context.Context, _ []gitops.Manifest, _ string, _ gitops.SyncOptions) (*gitops.SyncSummary, error) {
	return s.summary, s.err
}

func TestHandleSyncFromGitReportsSyncerFactoryError(t *testing.T) {
	setGitOpsTempDir(t)
	repo := createGitRepo(t, map[string]string{
		"manifests/app.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n",
	})
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://127.0.0.1:1",
	})
	factoryErr := errors.New("factory boom")
	server.newManifestSyncer = func(*rest.Config) (manifestSyncer, error) {
		return nil, factoryErr
	}

	got, err := server.handleSyncFromGit(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"repo":     repo,
		"path":     "manifests",
		"clusters": []string{"alpha"},
		"dry_run":  true,
	}))
	require.NoError(t, err)

	result := got.(*GitOpsSyncResult)
	require.Len(t, result.Summaries, 1)
	assert.Equal(t, "alpha", result.Summaries[0].Cluster)
	assert.Equal(t, 1, result.Summaries[0].Failed)
	require.Len(t, result.Summaries[0].Results, 1)
	assert.Equal(t, gitops.SyncActionFailed, result.Summaries[0].Results[0].Action)
	assert.Contains(t, result.Summaries[0].Results[0].Message, "Failed to create syncer")
	assert.Contains(t, result.Summaries[0].Results[0].Message, factoryErr.Error())
}

func TestHandleSyncFromGitReportsSyncError(t *testing.T) {
	setGitOpsTempDir(t)
	repo := createGitRepo(t, map[string]string{
		"manifests/app.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n",
	})
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://127.0.0.1:1",
	})
	syncErr := errors.New("sync boom")
	server.newManifestSyncer = func(*rest.Config) (manifestSyncer, error) {
		return &stubManifestSyncer{err: syncErr}, nil
	}

	got, err := server.handleSyncFromGit(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"repo":     repo,
		"path":     "manifests",
		"clusters": []string{"alpha"},
		"dry_run":  true,
	}))
	require.NoError(t, err)

	result := got.(*GitOpsSyncResult)
	require.Len(t, result.Summaries, 1)
	assert.Equal(t, "alpha", result.Summaries[0].Cluster)
	assert.Equal(t, 1, result.Summaries[0].Failed)
	require.Len(t, result.Summaries[0].Results, 1)
	assert.Equal(t, gitops.SyncActionFailed, result.Summaries[0].Results[0].Action)
	assert.Contains(t, result.Summaries[0].Results[0].Message, "Failed to sync")
	assert.Contains(t, result.Summaries[0].Results[0].Message, syncErr.Error())
}

func TestHandleSyncFromGitAppendsSuccessSummary(t *testing.T) {
	setGitOpsTempDir(t)
	repo := createGitRepo(t, map[string]string{
		"manifests/app.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n",
	})
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://127.0.0.1:1",
	})
	wantSummary := &gitops.SyncSummary{
		Cluster: "alpha",
		Created: 1,
		Results: []gitops.SyncResult{{
			Cluster: "alpha",
			Kind:    "ConfigMap",
			Name:    "demo",
			Action:  gitops.SyncActionCreated,
			Message: "created ConfigMap/demo",
		}},
	}
	server.newManifestSyncer = func(*rest.Config) (manifestSyncer, error) {
		return &stubManifestSyncer{summary: wantSummary}, nil
	}

	got, err := server.handleSyncFromGit(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"repo":     repo,
		"path":     "manifests",
		"clusters": []string{"alpha"},
		"dry_run":  true,
	}))
	require.NoError(t, err)

	result := got.(*GitOpsSyncResult)
	require.Len(t, result.Summaries, 1)
	assert.Equal(t, *wantSummary, result.Summaries[0])
}
