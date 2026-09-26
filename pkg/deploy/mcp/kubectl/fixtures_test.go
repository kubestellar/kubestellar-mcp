package kubectl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
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

type fakeDeps struct {
	clusters              []string
	discoverErr           error
	executeErr            error
	clusterErrors         map[string]error
	configErrs            map[string]error
	configs               map[string]*rest.Config
	selected              [][]string
	isSensitiveKind       func(kind string) bool
	sensitiveKindError    func(kind string) error
	manifestSensitiveKind func(doc string) (string, bool)
	isNamespaceKind       func(kind string) bool
	yamlToJSON            func(y string) string
	unstructuredFromYAML  func(y string, obj *unstructured.Unstructured) error
}

func newFakeDeps() *fakeDeps {
	return &fakeDeps{
		clusterErrors:         map[string]error{},
		configErrs:            map[string]error{},
		configs:               map[string]*rest.Config{},
		isSensitiveKind:       testIsSensitiveKind,
		sensitiveKindError:    testSensitiveKindError,
		manifestSensitiveKind: testManifestSensitiveKind,
		isNamespaceKind:       testIsNamespaceKind,
		yamlToJSON:            testYAMLToJSON,
		unstructuredFromYAML:  testUnstructuredFromYAML,
	}
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
		t.Fatalf("WriteToFile() error = %v", err)
	}

	manager, err := multicluster.NewClientManager(kubeconfig)
	if err != nil {
		t.Fatalf("NewClientManager() error = %v", err)
	}
	executor := multicluster.NewExecutor(manager)

	return Deps{
		DiscoverClusters:      manager.DiscoverClusters,
		ExecuteOnSelected:     executor.ExecuteOnSelected,
		GetConfig:             manager.GetConfig,
		IsSensitiveKind:       testIsSensitiveKind,
		SensitiveKindError:    testSensitiveKindError,
		ManifestSensitiveKind: testManifestSensitiveKind,
		IsNamespaceKind:       testIsNamespaceKind,
		YAMLToJSON:            testYAMLToJSON,
		UnstructuredFromYAML:  testUnstructuredFromYAML,
	}
}

func (f *fakeDeps) deps() Deps {
	return Deps{
		DiscoverClusters: func() ([]multicluster.ClusterInfo, error) {
			if f.discoverErr != nil {
				return nil, f.discoverErr
			}
			out := make([]multicluster.ClusterInfo, 0, len(f.clusters))
			for _, name := range f.clusters {
				out = append(out, multicluster.ClusterInfo{Name: name})
			}
			sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
			return out, nil
		},
		ExecuteOnSelected: func(ctx context.Context, clusterNames []string, fn multicluster.ExecuteFunc) ([]multicluster.ClusterResult, error) {
			if f.executeErr != nil {
				return nil, f.executeErr
			}
			f.selected = append(f.selected, append([]string(nil), clusterNames...))
			results := make([]multicluster.ClusterResult, 0, len(clusterNames))
			for _, name := range clusterNames {
				if err := f.clusterErrors[name]; err != nil {
					results = append(results, multicluster.ClusterResult{Cluster: name, Error: err.Error()})
					continue
				}
				result, err := fn(ctx, nil, name)
				if err != nil {
					results = append(results, multicluster.ClusterResult{Cluster: name, Error: err.Error()})
					continue
				}
				results = append(results, multicluster.ClusterResult{Cluster: name, Result: result})
			}
			return results, nil
		},
		GetConfig: func(clusterName string) (*rest.Config, error) {
			if err := f.configErrs[clusterName]; err != nil {
				return nil, err
			}
			if cfg := f.configs[clusterName]; cfg != nil {
				return cfg, nil
			}
			return &rest.Config{Host: "https://" + clusterName + ".example.com"}, nil
		},
		IsSensitiveKind:       f.isSensitiveKind,
		SensitiveKindError:    f.sensitiveKindError,
		ManifestSensitiveKind: f.manifestSensitiveKind,
		IsNamespaceKind:       f.isNamespaceKind,
		YAMLToJSON:            f.yamlToJSON,
		UnstructuredFromYAML:  f.unstructuredFromYAML,
	}
}

var testSensitiveKinds = map[string]bool{
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

func testIsSensitiveKind(kind string) bool {
	return testSensitiveKinds[strings.ToLower(kind)]
}

func testIsNamespaceKind(kind string) bool {
	switch strings.ToLower(kind) {
	case "namespace", "namespaces", "ns":
		return true
	default:
		return false
	}
}

func testSensitiveKindError(kind string) error {
	return fmt.Errorf("%q resources are blocked via MCP kubectl tools to prevent privilege escalation; use kubectl directly for this sensitive operation", kind)
}

func testManifestSensitiveKind(doc string) (string, bool) {
	doc = strings.TrimSpace(doc)
	if doc == "" {
		return "", false
	}

	obj := &unstructured.Unstructured{}
	if err := obj.UnmarshalJSON([]byte(testYAMLToJSON(doc))); err != nil {
		if err := testUnstructuredFromYAML(doc, obj); err != nil {
			return "", false
		}
	}

	kind := obj.GetKind()
	return kind, testIsSensitiveKind(kind)
}

func testYAMLToJSON(y string) string {
	data, err := k8syaml.ToJSON([]byte(y))
	if err != nil {
		return y
	}
	return string(data)
}

func testUnstructuredFromYAML(y string, obj *unstructured.Unstructured) error {
	data, err := k8syaml.ToJSON([]byte(y))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, obj)
}

type fakeK8sState struct {
	mu     sync.Mutex
	stored map[string]map[string]interface{}
}

type fakeAPIMode struct {
	failCreate bool
	failUpdate bool
}

func startFakeConfigMapAPI(t *testing.T, state *fakeK8sState, mode fakeAPIMode) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		const listPath = "/api/v1/namespaces/default/configmaps"
		path := r.URL.Path

		if path == listPath {
			if r.Method != http.MethodPost {
				http.Error(w, "unexpected method "+r.Method, http.StatusMethodNotAllowed)
				return
			}
			if mode.failCreate {
				http.Error(w, "create boom", http.StatusInternalServerError)
				return
			}
			var obj map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			meta, _ := obj["metadata"].(map[string]interface{})
			if meta == nil {
				meta = map[string]interface{}{}
				obj["metadata"] = meta
			}
			name, _ := meta["name"].(string)
			meta["resourceVersion"] = "1"

			state.mu.Lock()
			if state.stored == nil {
				state.stored = map[string]map[string]interface{}{}
			}
			state.stored[name] = obj
			state.mu.Unlock()

			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(obj)
			return
		}

		if strings.HasPrefix(path, listPath+"/") {
			name := strings.TrimPrefix(path, listPath+"/")

			state.mu.Lock()
			obj, ok := state.stored[name]
			state.mu.Unlock()

			switch r.Method {
			case http.MethodGet:
				if !ok {
					w.WriteHeader(http.StatusNotFound)
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"kind":       "Status",
						"apiVersion": "v1",
						"status":     "Failure",
						"reason":     "NotFound",
						"code":       404,
					})
					return
				}
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(obj)
				return
			case http.MethodPut:
				if mode.failUpdate {
					http.Error(w, "update boom", http.StatusInternalServerError)
					return
				}
				var updated map[string]interface{}
				if err := json.NewDecoder(r.Body).Decode(&updated); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				meta, _ := updated["metadata"].(map[string]interface{})
				if meta == nil {
					meta = map[string]interface{}{}
					updated["metadata"] = meta
				}
				meta["resourceVersion"] = "2"

				state.mu.Lock()
				state.stored[name] = updated
				state.mu.Unlock()

				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(updated)
				return
			default:
				http.Error(w, "unexpected method "+r.Method, http.StatusMethodNotAllowed)
				return
			}
		}

		http.Error(w, "unexpected path "+path, http.StatusNotFound)
	}))
}

func configForServer(server *httptest.Server) *rest.Config {
	return &rest.Config{Host: server.URL}
}

func countApplyStatuses(results []ApplyResult, want string) int {
	count := 0
	for _, result := range results {
		if result.Status == want {
			count++
		}
	}
	return count
}

func countDeleteStatuses(results []DeleteResult, want string) int {
	count := 0
	for _, result := range results {
		if result.Status == want {
			count++
		}
	}
	return count
}

func cloneConfigMap(obj map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(obj))
	for key, value := range obj {
		cloned[key] = value
	}
	return cloned
}
