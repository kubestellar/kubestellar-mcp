package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/kubestellar/kubestellar-mcp/pkg/gitops"
	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
	nsval "github.com/kubestellar/kubestellar-mcp/pkg/security/namespace"
)

func mustMarshalJSON(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return data
}

func itoa(n int) string { return strconv.Itoa(n) }

func clientForServer(t *testing.T, srv *httptest.Server) *kubernetes.Clientset {
	t.Helper()
	c, err := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})
	if err != nil {
		t.Fatalf("NewForConfig: %v", err)
	}
	return c
}

func mkDeployment(name, ns, appLabel string, wantReplicas, ready int32) appsv1.Deployment {
	r := wantReplicas
	labels := map[string]string{}
	if appLabel != "" {
		labels["app"] = appLabel
	}
	return appsv1.Deployment{
		TypeMeta:   metav1.TypeMeta{Kind: "Deployment", APIVersion: "apps/v1"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: labels},
		Spec:       appsv1.DeploymentSpec{Replicas: &r},
		Status:     appsv1.DeploymentStatus{ReadyReplicas: ready},
	}
}

// findAppFixtures holds items served by the fake apiserver for list ops.
type findAppFixtures struct {
	deployments  []appsv1.Deployment
	statefulsets []appsv1.StatefulSet
	daemonsets   []appsv1.DaemonSet
}

// startAppsServer serves deployment list requests (both cluster-scoped
// "all namespaces" and namespaced paths) plus a per-deployment PUT/PATCH
// handler used by scale/patch tests. Mirrors the equivalent root-package
// helper that pre-dated this extraction (tools_app_cluster_ops_test.go).
func startAppsServer(t *testing.T, fx findAppFixtures, updated map[string]*appsv1.Deployment) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path

		if strings.HasPrefix(p, "/apis/apps/v1/namespaces/") && strings.Contains(p, "/deployments/") &&
			(r.Method == http.MethodPut || r.Method == http.MethodPatch) {
			parts := strings.Split(strings.TrimPrefix(p, "/apis/apps/v1/namespaces/"), "/")
			if len(parts) < 3 {
				http.NotFound(w, r)
				return
			}
			name := parts[2]
			body, _ := io.ReadAll(r.Body)
			if r.Method == http.MethodPut {
				if updated != nil {
					updated[name] = &appsv1.Deployment{
						TypeMeta:   metav1.TypeMeta{Kind: "Deployment", APIVersion: "apps/v1"},
						ObjectMeta: metav1.ObjectMeta{Name: name, Annotations: map[string]string{"put-body-len": itoa(len(body))}},
					}
				}
				resp := appsv1.Deployment{
					TypeMeta:   metav1.TypeMeta{Kind: "Deployment", APIVersion: "apps/v1"},
					ObjectMeta: metav1.ObjectMeta{Name: name},
				}
				out, _ := json.Marshal(&resp)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(out)
				return
			}
			if updated != nil {
				updated[name] = &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: name, Annotations: map[string]string{"patch": string(body)}},
				}
			}
			_ = json.NewEncoder(w).Encode(&appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: name},
			})
			return
		}

		switch {
		case strings.HasSuffix(p, "/deployments"):
			list := appsv1.DeploymentList{
				TypeMeta: metav1.TypeMeta{Kind: "DeploymentList", APIVersion: "apps/v1"},
				Items:    fx.deployments,
			}
			_ = json.NewEncoder(w).Encode(&list)
		case strings.HasSuffix(p, "/statefulsets"):
			list := appsv1.StatefulSetList{
				TypeMeta: metav1.TypeMeta{Kind: "StatefulSetList", APIVersion: "apps/v1"},
				Items:    fx.statefulsets,
			}
			_ = json.NewEncoder(w).Encode(&list)
		case strings.HasSuffix(p, "/daemonsets"):
			list := appsv1.DaemonSetList{
				TypeMeta: metav1.TypeMeta{Kind: "DaemonSetList", APIVersion: "apps/v1"},
				Items:    fx.daemonsets,
			}
			_ = json.NewEncoder(w).Encode(&list)
		default:
			http.NotFound(w, r)
		}
	}))
}

// fakeDeps builds a Deps value with fully test-controlled function fields,
// mirroring the Deps fake pattern used by the kubectl sub-package
// (pkg/deploy/mcp/kubectl/fixtures_test.go).
type fakeDeps struct {
	clusters       []string
	discoverErr    error
	configs        map[string]*rest.Config
	configErrs     map[string]error
	capsForCluster func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (*multicluster.ClusterCapabilities, error)
	clusterCaps    func(ctx context.Context) ([]multicluster.ClusterCapabilities, error)
	findClusters   func(ctx context.Context, req multicluster.WorkloadRequirements) ([]string, error)
	execute        func(ctx context.Context, clusterName string, fn multicluster.ExecuteFunc) ([]multicluster.ClusterResult, error)
	executeSel     func(ctx context.Context, clusterNames []string, fn multicluster.ExecuteFunc) ([]multicluster.ClusterResult, error)
	manifestReader ManifestReader
	manifestSyncer func(config *rest.Config) (ManifestSyncer, error)
	validateErr    error
}

func newFakeDeps() *fakeDeps {
	return &fakeDeps{
		configs:    map[string]*rest.Config{},
		configErrs: map[string]error{},
	}
}

func (f *fakeDeps) deps() Deps {
	inClusters := func(name string) bool {
		for _, c := range f.clusters {
			if c == name {
				return true
			}
		}
		return false
	}
	execute := f.execute
	if execute == nil {
		execute = func(ctx context.Context, clusterName string, fn multicluster.ExecuteFunc) ([]multicluster.ClusterResult, error) {
			names := f.clusters
			if clusterName != "" {
				names = []string{clusterName}
			}
			results := make([]multicluster.ClusterResult, 0, len(names))
			for _, name := range names {
				if err := f.configErrs[name]; err != nil {
					results = append(results, multicluster.ClusterResult{Cluster: name, Error: err.Error()})
					continue
				}
				if clusterName != "" && !inClusters(name) {
					results = append(results, multicluster.ClusterResult{Cluster: name, Error: "cluster " + name + " not found"})
					continue
				}
				res, err := fn(ctx, nil, name)
				if err != nil {
					results = append(results, multicluster.ClusterResult{Cluster: name, Error: err.Error()})
					continue
				}
				results = append(results, multicluster.ClusterResult{Cluster: name, Result: res})
			}
			return results, nil
		}
	}
	executeSel := f.executeSel
	if executeSel == nil {
		executeSel = func(ctx context.Context, clusterNames []string, fn multicluster.ExecuteFunc) ([]multicluster.ClusterResult, error) {
			results := make([]multicluster.ClusterResult, 0, len(clusterNames))
			for _, name := range clusterNames {
				if err := f.configErrs[name]; err != nil {
					results = append(results, multicluster.ClusterResult{Cluster: name, Error: err.Error()})
					continue
				}
				res, err := fn(ctx, nil, name)
				if err != nil {
					results = append(results, multicluster.ClusterResult{Cluster: name, Error: err.Error()})
					continue
				}
				results = append(results, multicluster.ClusterResult{Cluster: name, Result: res})
			}
			return results, nil
		}
	}
	return Deps{
		Execute:           execute,
		ExecuteOnSelected: executeSel,
		DiscoverClusters: func() ([]multicluster.ClusterInfo, error) {
			if f.discoverErr != nil {
				return nil, f.discoverErr
			}
			out := make([]multicluster.ClusterInfo, 0, len(f.clusters))
			for _, name := range f.clusters {
				out = append(out, multicluster.ClusterInfo{Name: name})
			}
			return out, nil
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
		GetCapabilitiesForCluster: f.capsForCluster,
		GetClusterCapabilities:    f.clusterCaps,
		FindClustersForWorkload:   f.findClusters,
		GetManifestReader: func() ManifestReader {
			return f.manifestReader
		},
		GetManifestSyncer: f.manifestSyncer,
		ValidateManifestDocs: func(manifest string) error {
			return f.validateErr
		},
	}
}

// fakeManifestReader lets tests control ReadFromReader's outcome without a
// real *gitops.ManifestReader.
type fakeManifestReader struct {
	manifests []gitops.Manifest
	err       error
}

func (r *fakeManifestReader) ReadFromReader(io.Reader) ([]gitops.Manifest, error) {
	return r.manifests, r.err
}

type realManifestReaderAdapter struct {
	reader *gitops.ManifestReader
}

func (a realManifestReaderAdapter) ReadFromReader(r io.Reader) ([]gitops.Manifest, error) {
	return a.reader.ReadFromReader(r)
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
	return realDepsFromManager(manager)
}

func realDepsFromManager(manager *multicluster.ClientManager) Deps {
	executor := multicluster.NewExecutor(manager)
	selector := multicluster.NewSelector(executor)
	return Deps{
		Execute:                   executor.Execute,
		ExecuteOnSelected:         executor.ExecuteOnSelected,
		DiscoverClusters:          manager.DiscoverClusters,
		GetConfig:                 manager.GetConfig,
		GetCapabilitiesForCluster: selector.GetCapabilitiesForCluster,
		GetClusterCapabilities:    selector.GetClusterCapabilities,
		FindClustersForWorkload:   selector.FindClustersForWorkload,
		GetManifestReader: func() ManifestReader {
			return realManifestReaderAdapter{reader: gitops.NewManifestReaderWithSchemes(map[string]bool{
				"https": true,
				"http":  true,
				"file":  true,
			})}
		},
		GetManifestSyncer: func(config *rest.Config) (ManifestSyncer, error) {
			return gitops.NewSyncer(config)
		},
		ValidateManifestDocs: testValidateManifestDocs,
	}
}

func testValidateManifestDocs(manifest string) error {
	for _, doc := range strings.Split(manifest, "---") {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}
		var obj struct {
			Kind     string `json:"kind"`
			Metadata struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"metadata"`
		}
		// Mirror manifest_util.validateManifestDocs: it decodes into
		// unstructured.Unstructured, which rejects kind-less documents, so
		// those are skipped rather than namespace-validated.
		if err := parseYAMLForTest([]byte(doc), &obj); err != nil || obj.Kind == "" {
			continue
		}
		if isSensitiveKindForTest(obj.Kind) {
			return fmt.Errorf("%q resources are blocked via MCP kubectl tools to prevent privilege escalation; use kubectl directly for this sensitive operation", obj.Kind)
		}
		if isNamespaceKindForTest(obj.Kind) {
			if err := nsval.ValidateNamespace(obj.Metadata.Name); err != nil {
				return fmt.Errorf("invalid namespace in manifest: %w", err)
			}
		} else if obj.Metadata.Namespace != "" {
			if err := nsval.ValidateNamespace(obj.Metadata.Namespace); err != nil {
				return fmt.Errorf("invalid namespace in manifest: %w", err)
			}
		}
	}
	return nil
}

func parseYAMLForTest(data []byte, v interface{}) error {
	jsonData, err := yaml.ToJSON(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(jsonData, v)
}

func isSensitiveKindForTest(kind string) bool {
	switch strings.ToLower(kind) {
	case "clusterrole", "clusterroles", "clusterrolebinding", "clusterrolebindings",
		"role", "roles", "rolebinding", "rolebindings",
		"secret", "secrets", "serviceaccount", "serviceaccounts", "sa",
		"mutatingwebhookconfiguration", "mutatingwebhookconfigurations",
		"validatingwebhookconfiguration", "validatingwebhookconfigurations",
		"certificatesigningrequest", "certificatesigningrequests", "csr",
		"podsecuritypolicy", "podsecuritypolicies", "psp":
		return true
	default:
		return false
	}
}

func isNamespaceKindForTest(kind string) bool {
	switch strings.ToLower(kind) {
	case "namespace", "namespaces", "ns":
		return true
	default:
		return false
	}
}

func writeAppTestKubeconfig(t *testing.T, servers map[string]string) string {
	t.Helper()

	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("apiVersion: v1\nkind: Config\n")
	if len(names) > 0 {
		fmt.Fprintf(&b, "current-context: %s\n", names[0])
	}
	b.WriteString("clusters:\n")
	for _, name := range names {
		fmt.Fprintf(&b, "- name: %s\n  cluster:\n    server: %s\n    insecure-skip-tls-verify: true\n", name, servers[name])
	}
	b.WriteString("contexts:\n")
	for _, name := range names {
		fmt.Fprintf(&b, "- name: %s\n  context:\n    cluster: %s\n    user: user1\n", name, name)
	}
	b.WriteString("users:\n- name: user1\n  user:\n    token: abc\n")

	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	return path
}

func managerWithAppsServers(t *testing.T, perCluster map[string]findAppFixtures) (*multicluster.ClientManager, func()) {
	t.Helper()

	servers := make([]interface{ Close() }, 0, len(perCluster))
	urls := make(map[string]string, len(perCluster))
	for name, fx := range perCluster {
		srv := startAppsServer(t, fx, nil)
		servers = append(servers, srv)
		urls[name] = srv.URL
	}

	manager, err := multicluster.NewClientManager(writeAppTestKubeconfig(t, urls))
	if err != nil {
		for _, srv := range servers {
			srv.Close()
		}
		t.Fatalf("NewClientManager: %v", err)
	}
	return manager, func() {
		for _, srv := range servers {
			srv.Close()
		}
	}
}
