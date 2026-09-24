package kubectl

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
)

// sensitiveKinds mirrors the root package's blocklist (defined in
// pkg/deploy/mcp/manifest_util.go, which stays in the root package as a
// cross-domain shared helper per epic #983's non-goals). Kept in sync
// manually; the authoritative behavior is exercised end-to-end by the root
// package's own tests via the kubectl_adapter.go wiring.
var sensitiveKinds = map[string]bool{
	"clusterrole": true, "clusterroles": true,
	"clusterrolebinding": true, "clusterrolebindings": true,
	"role": true, "roles": true,
	"rolebinding": true, "rolebindings": true,
	"secret": true, "secrets": true,
	"serviceaccount": true, "serviceaccounts": true, "sa": true,
	"mutatingwebhookconfiguration": true, "mutatingwebhookconfigurations": true,
	"validatingwebhookconfiguration": true, "validatingwebhookconfigurations": true,
	"certificatesigningrequest": true, "certificatesigningrequests": true, "csr": true,
	"podsecuritypolicy": true, "podsecuritypolicies": true, "psp": true,
}

func fakeIsSensitiveKind(kind string) bool {
	return sensitiveKinds[strings.ToLower(kind)]
}

func fakeSensitiveKindError(kind string) error {
	return fmt.Errorf("%q resources are blocked via MCP kubectl tools to prevent privilege escalation; use kubectl directly for this sensitive operation", kind)
}

func fakeIsNamespaceKind(kind string) bool {
	switch strings.ToLower(kind) {
	case "namespace", "namespaces", "ns":
		return true
	default:
		return false
	}
}

func fakeYAMLToJSON(yamlStr string) string {
	data, err := k8syaml.ToJSON([]byte(yamlStr))
	if err != nil {
		return yamlStr
	}
	return string(data)
}

func fakeUnstructuredFromYAML(yamlStr string, obj *unstructured.Unstructured) error {
	data, err := k8syaml.ToJSON([]byte(yamlStr))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, obj)
}

func fakeManifestSensitiveKind(doc string) (string, bool) {
	doc = strings.TrimSpace(doc)
	if doc == "" {
		return "", false
	}
	obj := &unstructured.Unstructured{}
	if err := obj.UnmarshalJSON([]byte(fakeYAMLToJSON(doc))); err != nil {
		if err := fakeUnstructuredFromYAML(doc, obj); err != nil {
			return "", false
		}
	}
	kind := obj.GetKind()
	return kind, fakeIsSensitiveKind(kind)
}

// testDeps wires a real *multicluster.ClientManager (so DiscoverClusterNames
// and GetConfig behave exactly as they do in production) plus the fake
// sensitive-kind/manifest helpers above, satisfying the Deps interface.
type testDeps struct {
	manager  *multicluster.ClientManager
	executor *multicluster.Executor
}

func (d *testDeps) DiscoverClusterNames() ([]string, error) {
	clusters, err := d.manager.DiscoverClusters()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(clusters))
	for _, c := range clusters {
		names = append(names, c.Name)
	}
	return names, nil
}

func (d *testDeps) ExecuteOnSelected(ctx context.Context, clusterNames []string, fn multicluster.ExecuteFunc) ([]multicluster.ClusterResult, error) {
	return d.executor.ExecuteOnSelected(ctx, clusterNames, fn)
}

func (d *testDeps) GetConfig(clusterName string) (*rest.Config, error) {
	return d.manager.GetConfig(clusterName)
}

func (d *testDeps) IsSensitiveKind(kind string) bool     { return fakeIsSensitiveKind(kind) }
func (d *testDeps) SensitiveKindError(kind string) error { return fakeSensitiveKindError(kind) }
func (d *testDeps) IsNamespaceKind(kind string) bool     { return fakeIsNamespaceKind(kind) }
func (d *testDeps) ManifestSensitiveKind(doc string) (string, bool) {
	return fakeManifestSensitiveKind(doc)
}
func (d *testDeps) YAMLToJSON(yamlStr string) string { return fakeYAMLToJSON(yamlStr) }
func (d *testDeps) UnstructuredFromYAML(yamlStr string, obj *unstructured.Unstructured) error {
	return fakeUnstructuredFromYAML(yamlStr, obj)
}

var _ Deps = (*testDeps)(nil)

func newTestDeps(t *testing.T, contexts map[string]string) *testDeps {
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

	return &testDeps{manager: manager, executor: multicluster.NewExecutor(manager)}
}

func mustMarshalJSON(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return data
}
