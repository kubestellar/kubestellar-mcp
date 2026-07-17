package multicluster

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestNewSelector(t *testing.T) {
	exec := &Executor{}
	sel := NewSelector(exec)
	if sel == nil {
		t.Fatal("NewSelector returned nil")
	}
	if sel.executor != exec {
		t.Fatalf("NewSelector did not wire executor: got %p want %p", sel.executor, exec)
	}
}

// nodesHandler returns an http.Handler that serves the given nodes on
// GET /api/v1/nodes. Any other path returns 404.
func nodesHandler(t *testing.T, nodes []corev1.Node) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/nodes" {
			http.NotFound(w, r)
			return
		}
		list := corev1.NodeList{
			TypeMeta: metav1.TypeMeta{Kind: "NodeList", APIVersion: "v1"},
			Items:    nodes,
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(&list); err != nil {
			t.Errorf("encode nodes: %v", err)
		}
	})
}

func newClientForServer(t *testing.T, srv *httptest.Server) *kubernetes.Clientset {
	t.Helper()
	c, err := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})
	if err != nil {
		t.Fatalf("NewForConfig() error = %v", err)
	}
	return c
}

func mkNode(name string, ready bool, cpu, mem string, extras map[corev1.ResourceName]string, labels map[string]string) corev1.Node {
	status := corev1.NodeStatus{
		Capacity:    corev1.ResourceList{},
		Allocatable: corev1.ResourceList{},
		Conditions: []corev1.NodeCondition{{
			Type:   corev1.NodeReady,
			Status: corev1.ConditionFalse,
		}},
	}
	if ready {
		status.Conditions[0].Status = corev1.ConditionTrue
	}
	status.Capacity[corev1.ResourceCPU] = resource.MustParse(cpu)
	status.Capacity[corev1.ResourceMemory] = resource.MustParse(mem)
	status.Allocatable[corev1.ResourceCPU] = resource.MustParse(cpu)
	status.Allocatable[corev1.ResourceMemory] = resource.MustParse(mem)
	for k, v := range extras {
		status.Capacity[k] = resource.MustParse(v)
		status.Allocatable[k] = resource.MustParse(v)
	}
	return corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
		Status:     status,
	}
}

func TestGetCapabilitiesForCluster_AggregatesResources(t *testing.T) {
	nodes := []corev1.Node{
		mkNode("n1", true, "4", "8Gi",
			map[corev1.ResourceName]string{"nvidia.com/gpu": "2"},
			map[string]string{
				"topology.kubernetes.io/region": "us-east-1",
				"kubernetes.io/arch":            "amd64",
				"custom.example.com/team":       "core", // ignored
			},
		),
		mkNode("n2", false, "2", "4Gi", nil, nil), // not-ready branch
	}
	srv := httptest.NewServer(nodesHandler(t, nodes))
	defer srv.Close()

	sel := &Selector{}
	cap, err := sel.GetCapabilitiesForCluster(context.Background(), newClientForServer(t, srv), "prod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cap.Cluster != "prod" || cap.NodeCount != 2 || cap.ReadyNodes != 1 {
		t.Fatalf("unexpected counts: %+v", cap)
	}
	wantCPU := resource.MustParse("6")
	wantMem := resource.MustParse("12Gi")
	if got, want := cap.TotalCPU, (&wantCPU).String(); got != want {
		t.Fatalf("TotalCPU = %q want %q", got, want)
	}
	if got, want := cap.TotalMemory, (&wantMem).String(); got != want {
		t.Fatalf("TotalMemory = %q want %q", got, want)
	}
	if got, want := cap.AllocatableCPU, (&wantCPU).String(); got != want {
		t.Fatalf("AllocatableCPU = %q want %q", got, want)
	}
	if len(cap.GPUs) != 1 || cap.GPUs[0].Type != "nvidia.com/gpu" || cap.GPUs[0].Quantity != 2 {
		t.Fatalf("unexpected gpus: %+v", cap.GPUs)
	}
	if cap.Labels["topology.kubernetes.io/region"] != "us-east-1" {
		t.Fatalf("missing region label: %+v", cap.Labels)
	}
	if _, exists := cap.Labels["custom.example.com/team"]; exists {
		t.Fatalf("non-cluster label leaked: %+v", cap.Labels)
	}
}

func TestGetCapabilitiesForCluster_ListError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	sel := &Selector{}
	_, err := sel.GetCapabilitiesForCluster(context.Background(), newClientForServer(t, srv), "prod")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// managerWithServers builds a ClientManager whose clients each point at their
// own httptest server. Returned closer must be deferred.
func managerWithServers(t *testing.T, perCluster map[string][]corev1.Node) (*ClientManager, func()) {
	t.Helper()
	servers := make([]*httptest.Server, 0, len(perCluster))
	clients := make(map[string]*kubernetes.Clientset, len(perCluster))
	contexts := make(map[string]*clientcmdapi.Context, len(perCluster))
	clusterCfgs := make(map[string]*clientcmdapi.Cluster, len(perCluster))
	current := ""
	for name, nodes := range perCluster {
		s := httptest.NewServer(nodesHandler(t, nodes))
		servers = append(servers, s)
		clients[name] = newClientForServer(t, s)
		contexts[name] = &clientcmdapi.Context{Cluster: name}
		clusterCfgs[name] = &clientcmdapi.Cluster{Server: s.URL}
		if current == "" {
			current = name
		}
	}
	mgr := &ClientManager{
		clients: clients,
		rawConfig: clientcmdapi.Config{
			CurrentContext: current,
			Contexts:       contexts,
			Clusters:       clusterCfgs,
		},
		currentContext: current,
	}
	return mgr, func() {
		for _, s := range servers {
			s.Close()
		}
	}
}

func TestGetClusterCapabilities_MultiCluster(t *testing.T) {
	mgr, cleanup := managerWithServers(t, map[string][]corev1.Node{
		"alpha": {mkNode("a1", true, "8", "16Gi",
			map[corev1.ResourceName]string{"nvidia.com/gpu": "4"}, nil)},
		"beta": {mkNode("b1", true, "1", "1Gi", nil, nil)},
	})
	defer cleanup()

	sel := NewSelector(NewExecutor(mgr))
	caps, err := sel.GetClusterCapabilities(context.Background())
	if err != nil {
		t.Fatalf("GetClusterCapabilities: %v", err)
	}
	if len(caps) != 2 {
		t.Fatalf("expected 2 capabilities, got %d: %+v", len(caps), caps)
	}
	names := []string{caps[0].Cluster, caps[1].Cluster}
	sort.Strings(names)
	if names[0] != "alpha" || names[1] != "beta" {
		t.Fatalf("cluster names = %v", names)
	}
}

func TestGetClusterCapabilities_ErrorResultProducesEmptyEntry(t *testing.T) {
	// One good, one broken (500 on nodes.List)
	goodSrv := httptest.NewServer(nodesHandler(t, []corev1.Node{mkNode("g1", true, "2", "4Gi", nil, nil)}))
	defer goodSrv.Close()
	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer badSrv.Close()

	mgr := &ClientManager{
		clients: map[string]*kubernetes.Clientset{
			"good": newClientForServer(t, goodSrv),
			"bad":  newClientForServer(t, badSrv),
		},
		rawConfig: clientcmdapi.Config{
			CurrentContext: "good",
			Contexts: map[string]*clientcmdapi.Context{
				"good": {Cluster: "good"}, "bad": {Cluster: "bad"},
			},
			Clusters: map[string]*clientcmdapi.Cluster{
				"good": {Server: goodSrv.URL}, "bad": {Server: badSrv.URL},
			},
		},
		currentContext: "good",
	}

	sel := NewSelector(NewExecutor(mgr))
	caps, err := sel.GetClusterCapabilities(context.Background())
	if err != nil {
		t.Fatalf("GetClusterCapabilities: %v", err)
	}
	if len(caps) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(caps))
	}
	var goodCap, badCap *ClusterCapabilities
	for i := range caps {
		switch caps[i].Cluster {
		case "good":
			goodCap = &caps[i]
		case "bad":
			badCap = &caps[i]
		}
	}
	if goodCap == nil || goodCap.NodeCount != 1 {
		t.Fatalf("good entry unexpected: %+v", goodCap)
	}
	if badCap == nil || badCap.NodeCount != 0 || len(badCap.GPUs) != 0 {
		t.Fatalf("bad entry should be empty stub: %+v", badCap)
	}
}

func TestFindClustersForWorkload_FiltersByRequirements(t *testing.T) {
	mgr, cleanup := managerWithServers(t, map[string][]corev1.Node{
		"big": {mkNode("n", true, "16", "64Gi",
			map[corev1.ResourceName]string{"nvidia.com/gpu": "4"},
			map[string]string{"kubernetes.io/arch": "amd64"})},
		"small": {mkNode("n", true, "1", "1Gi", nil,
			map[string]string{"kubernetes.io/arch": "arm64"})},
	})
	defer cleanup()

	sel := NewSelector(NewExecutor(mgr))
	got, err := sel.FindClustersForWorkload(context.Background(), WorkloadRequirements{
		MinCPU:     "8",
		MinMemory:  "16Gi",
		GPUType:    "nvidia.com/gpu",
		MinGPU:     2,
		NodeLabels: map[string]string{"kubernetes.io/arch": "amd64"},
	})
	if err != nil {
		t.Fatalf("FindClustersForWorkload: %v", err)
	}
	if len(got) != 1 || got[0] != "big" {
		t.Fatalf("matching clusters = %v, want [big]", got)
	}
}

func TestFindClustersForWorkload_NoRequirementsReturnsAll(t *testing.T) {
	mgr, cleanup := managerWithServers(t, map[string][]corev1.Node{
		"alpha": {mkNode("n", true, "1", "1Gi", nil, nil)},
		"beta":  {mkNode("n", true, "1", "1Gi", nil, nil)},
	})
	defer cleanup()

	sel := NewSelector(NewExecutor(mgr))
	got, err := sel.FindClustersForWorkload(context.Background(), WorkloadRequirements{})
	if err != nil {
		t.Fatalf("FindClustersForWorkload: %v", err)
	}
	sort.Strings(got)
	if len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Fatalf("matching clusters = %v, want [alpha beta]", got)
	}
}
