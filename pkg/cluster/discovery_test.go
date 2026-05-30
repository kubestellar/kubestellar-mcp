package cluster

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestDiscoverClustersAndCurrentContext(t *testing.T) {
	kubeconfig := writeTestKubeconfig(t, map[string]string{
		"alpha": "https://alpha.example.com",
		"beta":  "https://beta.example.com",
	}, "beta")

	d := NewDiscoverer(kubeconfig)
	clusters, err := d.DiscoverClusters("all")
	if err != nil {
		t.Fatalf("DiscoverClusters() error = %v", err)
	}

	sort.Slice(clusters, func(i, j int) bool { return clusters[i].Name < clusters[j].Name })
	if len(clusters) != 2 {
		t.Fatalf("cluster count = %d, want 2", len(clusters))
	}

	if got := clusters[0]; got.Name != "alpha" || got.Server != "https://alpha.example.com" || got.Current || got.Source != "kubeconfig" || got.Status != "Unknown" {
		t.Fatalf("unexpected alpha cluster: %#v", got)
	}
	if got := clusters[1]; got.Name != "beta" || got.Server != "https://beta.example.com" || !got.Current || got.Source != "kubeconfig" || got.Status != "Unknown" {
		t.Fatalf("unexpected beta cluster: %#v", got)
	}

	currentContext, err := d.GetCurrentContext()
	if err != nil {
		t.Fatalf("GetCurrentContext() error = %v", err)
	}
	if currentContext != "beta" {
		t.Fatalf("current context = %q, want beta", currentContext)
	}
}

func TestDiscoverClustersInvalidKubeconfig(t *testing.T) {
	dir := newTestDir(t)
	kubeconfig := filepath.Join(dir, "broken-kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("clusters: ["), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := NewDiscoverer(kubeconfig).DiscoverClusters("kubeconfig")
	if err == nil || !strings.Contains(err.Error(), "failed to load kubeconfig") {
		t.Fatalf("DiscoverClusters() error = %v, want kubeconfig load failure", err)
	}
}

func TestCheckHealthByContext(t *testing.T) {
	tests := []struct {
		name                string
		versionStatus       int
		nodeStatus          int
		readyNodes          int
		totalNodes          int
		wantStatus          string
		wantAPIServerStatus string
		wantNodesReady      string
		wantMessage         string
	}{
		{
			name:                "healthy cluster",
			versionStatus:       http.StatusOK,
			nodeStatus:          http.StatusOK,
			readyNodes:          2,
			totalNodes:          2,
			wantStatus:          "Healthy",
			wantAPIServerStatus: "Healthy",
			wantNodesReady:      "2/2",
			wantMessage:         "All systems operational",
		},
		{
			name:                "degraded nodes",
			versionStatus:       http.StatusOK,
			nodeStatus:          http.StatusOK,
			readyNodes:          1,
			totalNodes:          2,
			wantStatus:          "Degraded",
			wantAPIServerStatus: "Healthy",
			wantNodesReady:      "1/2",
			wantMessage:         "1/2 nodes not ready",
		},
		{
			name:                "node list failure",
			versionStatus:       http.StatusOK,
			nodeStatus:          http.StatusInternalServerError,
			wantStatus:          "Degraded",
			wantAPIServerStatus: "Healthy",
			wantMessage:         "Failed to list nodes:",
		},
		{
			name:                "api server failure",
			versionStatus:       http.StatusInternalServerError,
			wantStatus:          "Unhealthy",
			wantAPIServerStatus: "Unreachable",
			wantMessage:         "Internal Server Error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newFakeClusterAPIServer(t, tt.versionStatus, tt.nodeStatus, tt.readyNodes, tt.totalNodes)
			defer server.Close()

			kubeconfig := writeTestKubeconfig(t, map[string]string{"alpha": server.URL}, "alpha")
			health, err := NewDiscoverer(kubeconfig).CheckHealthByContext("alpha")
			if err != nil {
				t.Fatalf("CheckHealthByContext() error = %v", err)
			}

			if health.Status != tt.wantStatus {
				t.Fatalf("Status = %q, want %q", health.Status, tt.wantStatus)
			}
			if health.APIServerStatus != tt.wantAPIServerStatus {
				t.Fatalf("APIServerStatus = %q, want %q", health.APIServerStatus, tt.wantAPIServerStatus)
			}
			if tt.wantNodesReady != "" && health.NodesReady != tt.wantNodesReady {
				t.Fatalf("NodesReady = %q, want %q", health.NodesReady, tt.wantNodesReady)
			}
			if !strings.Contains(health.Message, tt.wantMessage) {
				t.Fatalf("Message = %q, want substring %q", health.Message, tt.wantMessage)
			}
		})
	}
}

func newFakeClusterAPIServer(t *testing.T, versionStatus, nodeStatus, readyNodes, totalNodes int) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/version":
			if versionStatus != http.StatusOK {
				http.Error(w, http.StatusText(versionStatus), versionStatus)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"major":      "1",
				"minor":      "30",
				"gitVersion": "v1.30.0",
			})
		case "/api/v1/nodes":
			if nodeStatus != http.StatusOK {
				http.Error(w, "node list failed", nodeStatus)
				return
			}

			nodes := make([]corev1.Node, 0, totalNodes)
			for i := 0; i < totalNodes; i++ {
				status := corev1.ConditionFalse
				if i < readyNodes {
					status = corev1.ConditionTrue
				}
				nodes = append(nodes, corev1.Node{
					ObjectMeta: metav1.ObjectMeta{Name: "node-" + string(rune('a'+i))},
					Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{
						Type:   corev1.NodeReady,
						Status: status,
					}}},
				})
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(&corev1.NodeList{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "NodeList"},
				Items:    nodes,
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

func writeTestKubeconfig(t *testing.T, contexts map[string]string, currentContext string) string {
	t.Helper()

	config := clientcmdapi.NewConfig()
	config.CurrentContext = currentContext
	for name, server := range contexts {
		config.Contexts[name] = &clientcmdapi.Context{Cluster: name, AuthInfo: name}
		config.Clusters[name] = &clientcmdapi.Cluster{Server: server}
		config.AuthInfos[name] = &clientcmdapi.AuthInfo{}
	}

	dir := newTestDir(t)
	kubeconfig := filepath.Join(dir, "config")
	if err := clientcmd.WriteToFile(*config, kubeconfig); err != nil {
		t.Fatalf("WriteToFile() error = %v", err)
	}
	return kubeconfig
}

func newTestDir(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp(".", "cluster-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	return dir
}
