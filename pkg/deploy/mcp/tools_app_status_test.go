package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"

	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
)

// writeKubeconfig writes a kubeconfig file whose contexts point at the given
// server URLs. Uses insecure-skip-tls-verify since httptest servers are HTTP.
func writeKubeconfig(t *testing.T, servers map[string]string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("apiVersion: v1\nkind: Config\n")
	// Pick a deterministic current-context.
	names := make([]string, 0, len(servers))
	for n := range servers {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Fprintf(&b, "current-context: %s\n", names[0])
	b.WriteString("clusters:\n")
	for _, name := range names {
		fmt.Fprintf(&b, "- name: %s\n  cluster:\n    server: %s\n    insecure-skip-tls-verify: true\n", name, servers[name])
	}
	b.WriteString("contexts:\n")
	for _, name := range names {
		fmt.Fprintf(&b, "- name: %s\n  context:\n    cluster: %s\n    user: user1\n", name, name)
	}
	b.WriteString("users:\n- name: user1\n  user:\n    token: abc\n")
	dir := t.TempDir()
	path := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	return path
}

// managerWithAppsServers builds a real ClientManager backed by a temp
// kubeconfig, and starts one httptest apps/v1 server per cluster. `perCluster`
// maps cluster name -> fixtures for findAppInCluster.
func managerWithAppsServers(t *testing.T, perCluster map[string]findAppFixtures) (*multicluster.ClientManager, func()) {
	t.Helper()
	servers := make([]*httptest.Server, 0, len(perCluster))
	urls := make(map[string]string, len(perCluster))
	for name, fx := range perCluster {
		s := startAppsServer(t, fx, nil)
		servers = append(servers, s)
		urls[name] = s.URL
	}
	kc := writeKubeconfig(t, urls)
	mgr, err := multicluster.NewClientManager(kc)
	if err != nil {
		t.Fatalf("NewClientManager: %v", err)
	}
	return mgr, func() {
		for _, s := range servers {
			s.Close()
		}
	}
}

func managerBadServer(t *testing.T, clusterName string) (*multicluster.ClientManager, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	kc := writeKubeconfig(t, map[string]string{clusterName: srv.URL})
	mgr, err := multicluster.NewClientManager(kc)
	if err != nil {
		srv.Close()
		t.Fatalf("NewClientManager: %v", err)
	}
	return mgr, srv.Close
}

func newServerWithManager(mgr *multicluster.ClientManager) *Server {
	exec := multicluster.NewExecutor(mgr)
	return &Server{manager: mgr, executor: exec, selector: multicluster.NewSelector(exec)}
}

func decodeAppStatus(t *testing.T, res interface{}) AppStatus {
	t.Helper()
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out AppStatus
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func TestHandleGetAppStatus_InvalidAppName(t *testing.T) {
	srv := &Server{}
	_, err := srv.handleGetAppStatus(context.Background(), json.RawMessage(`{"app":"bad;name"}`))
	if err == nil {
		t.Fatal("expected invalid-app-name error")
	}
}

func TestHandleGetAppStatus_InvalidNamespace(t *testing.T) {
	srv := &Server{}
	if _, err := srv.handleGetAppStatus(context.Background(), json.RawMessage(`{"app":"demo","namespace":"Invalid_NS"}`)); err == nil {
		t.Fatal("expected invalid-namespace error (validation)")
	}
	if _, err := srv.handleGetAppStatus(context.Background(), json.RawMessage(`{"app":"demo","namespace":"kube-system"}`)); err == nil {
		t.Fatal("expected invalid-namespace error (protected)")
	}
}

func TestHandleGetAppStatus_MalformedJSON(t *testing.T) {
	srv := &Server{}
	if _, err := srv.handleGetAppStatus(context.Background(), json.RawMessage(`{`)); err == nil {
		t.Fatal("expected invalid-arguments error")
	}
}

func TestHandleGetAppStatus_HealthyAcrossClusters(t *testing.T) {
	mgr, cleanup := managerWithAppsServers(t, map[string]findAppFixtures{
		"cA": {deployments: []appsv1.Deployment{mkDeployment("demo-web", "app", "demo", 3, 3)}},
		"cB": {deployments: []appsv1.Deployment{mkDeployment("demo-api", "app", "demo", 2, 2)}},
	})
	defer cleanup()

	srv := newServerWithManager(mgr)
	res, err := srv.handleGetAppStatus(context.Background(), json.RawMessage(`{"app":"demo"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	status := decodeAppStatus(t, res)
	if status.OverallStatus != "healthy" {
		t.Fatalf("OverallStatus = %q, want healthy: %+v", status.OverallStatus, status)
	}
	if status.TotalClusters != 2 || status.HealthyClusters != 2 {
		t.Fatalf("cluster counts = %d/%d: %+v", status.HealthyClusters, status.TotalClusters, status)
	}
	if status.TotalReplicas != 5 || status.ReadyReplicas != 5 {
		t.Fatalf("replica counts = %d/%d", status.ReadyReplicas, status.TotalReplicas)
	}
	if len(status.Issues) != 0 {
		t.Fatalf("healthy result should have no issues, got %v", status.Issues)
	}
}

func TestHandleGetAppStatus_DegradedMixed(t *testing.T) {
	mgr, cleanup := managerWithAppsServers(t, map[string]findAppFixtures{
		"cA": {deployments: []appsv1.Deployment{mkDeployment("demo-web", "app", "demo", 3, 3)}},                                       // healthy
		"cB": {statefulsets: []appsv1.StatefulSet{mkStatefulSet("demo-db", "app", "demo", 3, 1)}, deployments: []appsv1.Deployment{}}, // degraded (1/3 ready)
	})
	defer cleanup()

	srv := newServerWithManager(mgr)
	res, err := srv.handleGetAppStatus(context.Background(), json.RawMessage(`{"app":"demo"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	status := decodeAppStatus(t, res)
	if status.OverallStatus != "degraded" {
		t.Fatalf("OverallStatus = %q, want degraded: %+v", status.OverallStatus, status)
	}
	if status.HealthyClusters != 1 || status.TotalClusters != 2 {
		t.Fatalf("counts = %d/%d", status.HealthyClusters, status.TotalClusters)
	}
	// Issue string must include the degraded cluster/name.
	found := false
	for _, msg := range status.Issues {
		if strings.Contains(msg, "cB/demo-db") && strings.Contains(msg, "degraded") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected Issues to contain cB/demo-db degraded: %v", status.Issues)
	}
}

func TestHandleGetAppStatus_AllFailedYieldsFailedOverall(t *testing.T) {
	mgr, cleanup := managerWithAppsServers(t, map[string]findAppFixtures{
		"cA": {deployments: []appsv1.Deployment{mkDeployment("demo-web", "app", "demo", 3, 0)}}, // failed
	})
	defer cleanup()

	srv := newServerWithManager(mgr)
	res, err := srv.handleGetAppStatus(context.Background(), json.RawMessage(`{"app":"demo"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	status := decodeAppStatus(t, res)
	if status.OverallStatus != "failed" {
		t.Fatalf("OverallStatus = %q, want failed: %+v", status.OverallStatus, status)
	}
	if status.HealthyClusters != 0 || status.TotalClusters != 1 {
		t.Fatalf("counts = %d/%d", status.HealthyClusters, status.TotalClusters)
	}
}

func TestHandleGetAppStatus_NotFoundWhenNoInstances(t *testing.T) {
	mgr, cleanup := managerWithAppsServers(t, map[string]findAppFixtures{
		"cA": {deployments: []appsv1.Deployment{mkDeployment("other", "app", "other", 1, 1)}},
	})
	defer cleanup()

	srv := newServerWithManager(mgr)
	res, err := srv.handleGetAppStatus(context.Background(), json.RawMessage(`{"app":"demo"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	status := decodeAppStatus(t, res)
	if status.OverallStatus != "not found" {
		t.Fatalf("OverallStatus = %q, want 'not found': %+v", status.OverallStatus, status)
	}
	if status.TotalClusters != 0 {
		t.Fatalf("TotalClusters = %d, want 0", status.TotalClusters)
	}
}

func TestHandleGetAppStatus_BrokenClusterYieldsNotFound(t *testing.T) {
	// findAppInCluster deliberately swallows list errors so that a partial
	// outage on one cluster doesn't break the whole call. Verify that a
	// bad cluster is treated as a no-instance result (OverallStatus="not found")
	// rather than surfacing as an executor error.
	mgr, cleanup := managerBadServer(t, "brokenCluster")
	defer cleanup()

	srv := newServerWithManager(mgr)
	res, err := srv.handleGetAppStatus(context.Background(), json.RawMessage(`{"app":"demo"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	status := decodeAppStatus(t, res)
	if status.OverallStatus != "not found" {
		t.Fatalf("OverallStatus = %q, want 'not found': %+v", status.OverallStatus, status)
	}
	if status.TotalClusters != 0 {
		t.Fatalf("TotalClusters = %d, want 0", status.TotalClusters)
	}
}

// Guards against the aggregation loop dropping data ordering — sort by cluster
// before inspecting instances, so we compare deterministically.
func TestHandleGetAppStatus_InstancesFromEveryCluster(t *testing.T) {
	mgr, cleanup := managerWithAppsServers(t, map[string]findAppFixtures{
		"c1": {deployments: []appsv1.Deployment{mkDeployment("demo", "app", "demo", 1, 1)}},
		"c2": {deployments: []appsv1.Deployment{mkDeployment("demo", "app", "demo", 1, 1)}},
		"c3": {deployments: []appsv1.Deployment{mkDeployment("demo", "app", "demo", 1, 1)}},
	})
	defer cleanup()

	srv := newServerWithManager(mgr)
	res, err := srv.handleGetAppStatus(context.Background(), json.RawMessage(`{"app":"demo"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	status := decodeAppStatus(t, res)
	if len(status.Instances) != 3 {
		t.Fatalf("expected 3 instances, got %d: %+v", len(status.Instances), status.Instances)
	}
	got := []string{status.Instances[0].Cluster, status.Instances[1].Cluster, status.Instances[2].Cluster}
	sort.Strings(got)
	if got[0] != "c1" || got[1] != "c2" || got[2] != "c3" {
		t.Fatalf("clusters in instances = %v, want [c1 c2 c3]", got)
	}
}
