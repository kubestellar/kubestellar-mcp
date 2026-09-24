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
// covering the given contexts). Despite the name (inherited from the
// pre-refactor tools_helm_fixtures_test.go, before the pkg/deploy/mcp/helm
// extraction in epic #983), this fixture is used broadly across the root
// package's tests (server_test.go, server_protocol_test.go,
// handle_tool_call_dispatch_test.go, tools_deploy_*_test.go,
// tools_kubectl_*_test.go, tools_labels_discover_clusters_test.go,
// tools_scale_uncovered_test.go), not just the helm-domain tests (which now
// have their own equivalent fixture in pkg/deploy/mcp/helm/fixtures_test.go).
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

// mustMarshalJSON is used broadly by the root package's tests to build tool
// call arguments. Retained here after the pkg/deploy/mcp/helm extraction
// (epic #983) since non-helm tests (tools_deploy_*_test.go,
// tools_kubectl_*_test.go, tools_labels_discover_clusters_test.go,
// tools_scale_uncovered_test.go) depend on it too.
func mustMarshalJSON(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return data
}
