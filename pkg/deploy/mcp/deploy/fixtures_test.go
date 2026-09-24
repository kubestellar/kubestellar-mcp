package deploy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/kubestellar/kubestellar-mcp/pkg/gitops"
	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
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
	deployments []appsv1.Deployment
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

		if strings.HasSuffix(p, "/deployments") {
			list := appsv1.DeploymentList{
				TypeMeta: metav1.TypeMeta{Kind: "DeploymentList", APIVersion: "apps/v1"},
				Items:    fx.deployments,
			}
			_ = json.NewEncoder(w).Encode(&list)
			return
		}

		http.NotFound(w, r)
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
