package kustomize

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// fakeValidateClusters stands in for the root package's validateHelmClusters
// (pkg/deploy/mcp/tools_helm_validate.go), which stays in the root package as
// a cross-domain shared helper per epic #983's non-goals. It preserves the
// one behavior these tests depend on: rejecting cluster names that look like
// injected flags.
func fakeValidateClusters(clusters []string) error {
	for _, c := range clusters {
		if strings.HasPrefix(c, "-") {
			return fmt.Errorf("cluster %q must not begin with '-' (possible flag injection)", c)
		}
	}
	return nil
}

// fakeValidateManifest stands in for the root package's validateManifestDocs
// (pkg/deploy/mcp/manifest_util.go), which stays in the root package as a
// cross-domain shared helper per epic #983's non-goals. It preserves the one
// behavior these tests depend on: rejecting a built manifest that contains a
// blocked/sensitive kind such as Secret.
func fakeValidateManifest(manifest string) error {
	for _, doc := range strings.Split(manifest, "---") {
		if strings.Contains(doc, "kind: Secret") {
			return fmt.Errorf("%q resources are blocked via MCP kubectl tools to prevent privilege escalation; use kubectl directly for this sensitive operation", "Secret")
		}
	}
	return nil
}

// newTestDeps builds a Deps wired to a real *multicluster.ClientManager
// (so DiscoverClusters behaves exactly as it does in production) plus the
// fake validators above.
func newTestDeps(t *testing.T, contexts map[string]string) Deps {
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

	return Deps{
		DiscoverClusters: manager.DiscoverClusters,
		ValidateClusters: fakeValidateClusters,
		ValidateManifest: fakeValidateManifest,
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

func readLogFile(t *testing.T, logFile string) string {
	t.Helper()
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	return string(data)
}
