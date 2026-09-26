package labels

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func mustMarshalJSON(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return data
}

func clientForServer(t *testing.T, srv *httptest.Server) *kubernetes.Clientset {
	t.Helper()
	client, err := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})
	if err != nil {
		t.Fatalf("kubernetes.NewForConfig: %v", err)
	}
	return client
}

func decodeLabelsResp(t *testing.T, res interface{}) map[string]interface{} {
	t.Helper()
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	return out
}

func decodeLabelPatch(t *testing.T, patch []byte) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	if err := json.Unmarshal(patch, &out); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	return out
}

func labelResultsStatuses(m map[string]interface{}) []string {
	items, _ := m["results"].([]interface{})
	out := make([]string, 0, len(items))
	for _, item := range items {
		result, _ := item.(map[string]interface{})
		status, _ := result["status"].(string)
		out = append(out, status)
	}
	return out
}

type fakeDeps struct {
	clusterNames []string
	discoverErr  error
	clients      map[string]*kubernetes.Clientset
	execErrs     map[string]error
	execAllErr   error
	sensitive    map[string]bool
	selected     [][]string
}

func (d *fakeDeps) DiscoverClusterNames() ([]string, error) {
	if d.discoverErr != nil {
		return nil, d.discoverErr
	}
	out := append([]string(nil), d.clusterNames...)
	sort.Strings(out)
	return out, nil
}

func (d *fakeDeps) ExecuteOnSelected(ctx context.Context, clusterNames []string, fn multicluster.ExecuteFunc) ([]multicluster.ClusterResult, error) {
	if d.execAllErr != nil {
		return nil, d.execAllErr
	}
	d.selected = append(d.selected, append([]string(nil), clusterNames...))
	results := make([]multicluster.ClusterResult, 0, len(clusterNames))
	for _, name := range clusterNames {
		if err := d.execErrs[name]; err != nil {
			results = append(results, multicluster.ClusterResult{Cluster: name, Error: err.Error()})
			continue
		}
		client := d.clients[name]
		result, err := fn(ctx, client, name)
		if err != nil {
			results = append(results, multicluster.ClusterResult{Cluster: name, Error: err.Error()})
			continue
		}
		results = append(results, multicluster.ClusterResult{Cluster: name, Result: result})
	}
	return results, nil
}

func (d *fakeDeps) IsSensitiveKind(kind string) bool {
	return d.sensitive[strings.ToLower(kind)]
}

func (d *fakeDeps) SensitiveKindError(kind string) error {
	return fmt.Errorf("%q resources are blocked via MCP kubectl tools to prevent privilege escalation; use kubectl directly for this sensitive operation", kind)
}

type realDeps struct {
	manager  *multicluster.ClientManager
	executor *multicluster.Executor
}

func newRealDeps(t *testing.T, contexts map[string]string) Deps {
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

	kubeconfig := filepath.Join(t.TempDir(), "config")
	if err := clientcmd.WriteToFile(*config, kubeconfig); err != nil {
		t.Fatalf("clientcmd.WriteToFile: %v", err)
	}

	manager, err := multicluster.NewClientManager(kubeconfig)
	if err != nil {
		t.Fatalf("multicluster.NewClientManager: %v", err)
	}

	return &realDeps{
		manager:  manager,
		executor: multicluster.NewExecutor(manager),
	}
}

func (d *realDeps) DiscoverClusterNames() ([]string, error) {
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

func (d *realDeps) ExecuteOnSelected(ctx context.Context, clusterNames []string, fn multicluster.ExecuteFunc) ([]multicluster.ClusterResult, error) {
	return d.executor.ExecuteOnSelected(ctx, clusterNames, fn)
}

var sensitiveKinds = map[string]bool{
	"clusterrole":                     true,
	"clusterroles":                    true,
	"clusterrolebinding":              true,
	"clusterrolebindings":             true,
	"role":                            true,
	"roles":                           true,
	"rolebinding":                     true,
	"rolebindings":                    true,
	"secret":                          true,
	"secrets":                         true,
	"serviceaccount":                  true,
	"serviceaccounts":                 true,
	"sa":                              true,
	"mutatingwebhookconfiguration":    true,
	"mutatingwebhookconfigurations":   true,
	"validatingwebhookconfiguration":  true,
	"validatingwebhookconfigurations": true,
	"certificatesigningrequest":       true,
	"certificatesigningrequests":      true,
	"csr":                             true,
	"podsecuritypolicy":               true,
	"podsecuritypolicies":             true,
	"psp":                             true,
}

func (d *realDeps) IsSensitiveKind(kind string) bool {
	return sensitiveKinds[strings.ToLower(kind)]
}

func (d *realDeps) SensitiveKindError(kind string) error {
	return fmt.Errorf("%q resources are blocked via MCP kubectl tools to prevent privilege escalation; use kubectl directly for this sensitive operation", kind)
}

func patchAcceptingServer(t *testing.T, recorded *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			http.NotFound(w, r)
			return
		}
		if recorded != nil {
			*recorded = append(*recorded, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
}

func startNotFoundServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"NotFound","message":"deployments.apps \"demo\" not found","code":404}`))
	}))
}

func startServerErrServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
}
