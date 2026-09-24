package gitops

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	upstreamgitops "github.com/kubestellar/kubestellar-mcp/pkg/gitops"
	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// newTestServer builds a *Server backed by a real *multicluster.ClientManager
// (constructed from an in-memory kubeconfig covering the given contexts) and
// the real gitops.NewSyncer/NewDriftDetector/NewManifestReaderWithSchemes
// factories, mirroring the pre-refactor newHelmTestServer fixture in
// pkg/deploy/mcp. file:// URLs are allowed so tests can exercise local git
// repos without a real remote.
func newTestServer(t *testing.T, contexts map[string]string) *Server {
	t.Helper()

	config := clientcmdapi.NewConfig()
	firstContext := ""
	for name, serverURL := range contexts {
		if firstContext == "" {
			firstContext = name
		}
		config.Contexts[name] = &clientcmdapi.Context{Cluster: name, AuthInfo: name}
		config.Clusters[name] = &clientcmdapi.Cluster{Server: serverURL}
		config.AuthInfos[name] = &clientcmdapi.AuthInfo{}
	}
	config.CurrentContext = firstContext

	dir := t.TempDir()

	kubeconfig := filepath.Join(dir, "config")
	if err := clientcmd.WriteToFile(*config, kubeconfig); err != nil {
		t.Fatalf("WriteToFile() error = %v", err)
	}

	manager, err := multicluster.NewClientManager(kubeconfig)
	if err != nil {
		t.Fatalf("NewClientManager() error = %v", err)
	}

	return &Server{
		Access: manager,
		NewManifestReader: func() *upstreamgitops.ManifestReader {
			return upstreamgitops.NewManifestReaderWithSchemes(map[string]bool{
				"https": true,
				"http":  true,
				"file":  true,
			})
		},
		NewManifestSyncer: func(c *rest.Config) (ManifestSyncer, error) {
			return upstreamgitops.NewSyncer(c)
		},
		NewDriftDetector: func(c *rest.Config) (DriftDetector, error) {
			return upstreamgitops.NewDriftDetector(c)
		},
	}
}

func mustMarshalJSON(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return data
}

func setGitOpsTempDir(t *testing.T) {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())
}

func createGitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	absDir := t.TempDir()

	for name, content := range files {
		path := filepath.Join(absDir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}

	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = absDir
		output, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "git %v failed: %s", args, string(output))
	}

	runGit("init", "-b", "main")
	runGit("config", "user.name", "Copilot Test")
	runGit("config", "user.email", "copilot@example.com")
	runGit("add", ".")
	if len(files) == 0 {
		runGit("commit", "--allow-empty", "-m", "test repo")
	} else {
		runGit("commit", "-m", "test repo")
	}

	return "file://" + absDir
}
