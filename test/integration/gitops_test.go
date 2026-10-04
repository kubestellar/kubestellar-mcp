//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/gitops"
	upstreamgitops "github.com/kubestellar/kubestellar-mcp/pkg/gitops"
	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
)

// makeGitopsLocalRepo initialises a real git repository at a fresh temp dir
// containing the given files, then returns the file:// URL that ReadFromGit
// can clone. Mirrors pkg/gitops/manifest_readfromgit_clone_test.go's
// makeLocalRepo, which is how the package's own unit tests exercise the
// clone path without a network round-trip. Skips if git is unavailable.
func makeGitopsLocalRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available in PATH")
	}
	dir := t.TempDir()
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@example.invalid",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@example.invalid",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "safe.directory", dir)

	for rel, content := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", full, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
	run("add", "-A")
	run("commit", "-q", "-m", "seed")

	return "file://" + dir
}

// newGitopsServer wires a pkg/deploy/mcp/gitops.Server to the shared envtest
// apiserver, mirroring how pkg/deploy/mcp/gitops_adapter.go wires the
// production *Server's multicluster manager and factories, but with a
// single hard-coded cluster and a file://-scheme manifest reader so the
// "clone from git" half of sync_from_git/reconcile/preview_changes can run
// against a real local repo instead of a network source.
func newGitopsServer() *gitops.Server {
	return &gitops.Server{
		Access: gitopsSingleClusterAccess{},
		NewManifestReader: func() *upstreamgitops.ManifestReader {
			return upstreamgitops.NewManifestReaderWithSchemes(map[string]bool{"file": true})
		},
		NewManifestSyncer: func(config *rest.Config) (gitops.ManifestSyncer, error) {
			return upstreamgitops.NewSyncer(config)
		},
		NewDriftDetector: func(config *rest.Config) (gitops.DriftDetector, error) {
			return upstreamgitops.NewDriftDetector(config)
		},
	}
}

// gitopsSingleClusterAccess implements gitops.ClusterAccess with the single
// synthetic "envtest" cluster shared by this suite.
type gitopsSingleClusterAccess struct{}

func (gitopsSingleClusterAccess) DiscoverClusters() ([]multicluster.ClusterInfo, error) {
	return []multicluster.ClusterInfo{{Name: integrationClusterName}}, nil
}

func (gitopsSingleClusterAccess) GetConfig(clusterName string) (*rest.Config, error) {
	return testCfg, nil
}

// TestGitopsSyncFromGit exercises sync_from_git end-to-end: a real local git
// repository (cloned via ReadFromGit's file:// path) supplies a ConfigMap
// manifest that the real gitops.Syncer applies through a dynamic client
// against the envtest apiserver. This covers the real-apiserver create/
// update round-trip (RESTMapper discovery, dynamic-client Create/Update)
// that sync_from_git's own unit tests fake via a stubbed ManifestSyncer.
func TestGitopsSyncFromGit(t *testing.T) {
	ctx := context.Background()

	clientset, err := kubernetes.NewForConfig(testCfg)
	require.NoError(t, err, "kubernetes.NewForConfig")

	const namespace = "mcp-integration-gitops"
	const cmName = "mcp-integration-gitops-cm"

	_, err = clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}, metav1.CreateOptions{})
	require.NoError(t, err, "create namespace")

	repoURL := makeGitopsLocalRepo(t, map[string]string{
		"manifests/cm.yaml": strings.TrimSpace(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: `+cmName+`
  namespace: `+namespace+`
data:
  key: from-git
`) + "\n",
	})

	srv := newGitopsServer()
	handler := srv.Tools()
	var syncFromGit, reconcile, previewChanges func(ctx context.Context, args json.RawMessage) (interface{}, error)
	for _, td := range handler {
		switch td.Name {
		case "sync_from_git":
			syncFromGit = td.Handler
		case "reconcile":
			reconcile = td.Handler
		case "preview_changes":
			previewChanges = td.Handler
		}
	}
	require.NotNil(t, syncFromGit, "sync_from_git tool not registered")
	require.NotNil(t, reconcile, "reconcile tool not registered")
	require.NotNil(t, previewChanges, "preview_changes tool not registered")

	args, err := json.Marshal(map[string]interface{}{
		"repo": repoURL,
		"path": "manifests/",
	})
	require.NoError(t, err)

	result, err := syncFromGit(ctx, args)
	require.NoError(t, err, "sync_from_git")
	syncResult, ok := result.(*gitops.SyncResult)
	require.True(t, ok, "sync_from_git result type")
	require.Len(t, syncResult.Summaries, 1, "expected one cluster summary")
	require.Equal(t, 1, syncResult.Summaries[0].Created, "expected the ConfigMap to be created")

	cm, err := clientset.CoreV1().ConfigMaps(namespace).Get(ctx, cmName, metav1.GetOptions{})
	require.NoError(t, err, "ConfigMap should exist in the cluster after sync_from_git")
	require.Equal(t, "from-git", cm.Data["key"])

	t.Run("reconcile re-applies and reports unchanged", func(t *testing.T) {
		reconcileArgs, err := json.Marshal(map[string]interface{}{
			"repo": repoURL,
			"path": "manifests/",
		})
		require.NoError(t, err)

		result, err := reconcile(ctx, reconcileArgs)
		require.NoError(t, err, "reconcile")
		syncResult, ok := result.(*gitops.SyncResult)
		require.True(t, ok, "reconcile result type")
		require.False(t, syncResult.DryRun, "reconcile must not be a dry run")
		require.Len(t, syncResult.Summaries, 1)
		require.Equal(t, 1, syncResult.Summaries[0].Unchanged, "re-applying an identical manifest should report unchanged")
	})

	t.Run("preview_changes reports unchanged without mutating the cluster", func(t *testing.T) {
		previewArgs, err := json.Marshal(map[string]interface{}{
			"repo": repoURL,
			"path": "manifests/",
		})
		require.NoError(t, err)

		result, err := previewChanges(ctx, previewArgs)
		require.NoError(t, err, "preview_changes")
		syncResult, ok := result.(*gitops.SyncResult)
		require.True(t, ok, "preview_changes result type")
		require.True(t, syncResult.DryRun, "preview_changes must be a dry run")
		require.Len(t, syncResult.Summaries, 1)
		require.Equal(t, 1, syncResult.Summaries[0].Unchanged)

		cm, err := clientset.CoreV1().ConfigMaps(namespace).Get(ctx, cmName, metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, "from-git", cm.Data["key"], "preview_changes must not mutate the cluster")
	})

	t.Run("rejects a call without repo", func(t *testing.T) {
		_, err := syncFromGit(ctx, json.RawMessage(`{}`))
		require.Error(t, err)
		require.Contains(t, err.Error(), "repo is required")
	})
}
