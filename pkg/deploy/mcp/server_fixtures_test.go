package mcp

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/gitops"
	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// newHelmTestServer builds a *Server backed by a real
// *multicluster.ClientManager (constructed from an in-memory kubeconfig
// covering the given contexts). The name is inherited from the pre-#983
// helm fixtures; it is now the shared fixture for the root package's
// protocol, dispatch, and adapter-wiring tests.
func newHelmTestServer(t *testing.T, contexts map[string]string) *Server {
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

	executor := multicluster.NewExecutor(manager)
	selector := multicluster.NewSelector(executor)

	return &Server{
		manager:  manager,
		executor: executor,
		selector: selector,
		newManifestReader: func() *gitops.ManifestReader {
			return gitops.NewManifestReaderWithSchemes(map[string]bool{
				"https": true,
				"http":  true,
				"file":  true,
			})
		},
	}
}

// mustMarshalJSON builds tool call arguments for the root package's tests.
func mustMarshalJSON(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return data
}
