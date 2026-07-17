package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
	appsv1 "k8s.io/api/apps/v1"
)

// executeResultsCount extracts the number of ExecuteResult items in
// handlePatchApp's response map.
func executeResultsCount(t *testing.T, res interface{}) int {
	t.Helper()
	m, ok := res.(map[string]interface{})
	if !ok {
		t.Fatalf("res = %#v, want map", res)
	}
	items, ok := m["results"].([]multicluster.ClusterResult)
	if !ok {
		t.Fatalf("results field = %#v (%T), want []multicluster.ClusterResult", m["results"], m["results"])
	}
	return len(items)
}

func TestHandlePatchApp_InvalidArgs(t *testing.T) {
	s := &Server{}
	if _, err := s.handlePatchApp(context.Background(), json.RawMessage(`{bad`)); err == nil {
		t.Fatal("expected invalid arguments error")
	}
}

// TestHandlePatchApp_TwoClustersStrategic verifies the full multi-cluster
// executor fan-out with the default (strategic) patch type.
func TestHandlePatchApp_TwoClustersStrategic(t *testing.T) {
	mgr, cleanup := managerWithAppsServers(t, map[string]findAppFixtures{
		"cA": {deployments: []appsv1.Deployment{mkDeployment("demo", "apps", "demo", 3, 3)}},
		"cB": {deployments: []appsv1.Deployment{mkDeployment("demo", "apps", "demo", 2, 2)}},
	})
	defer cleanup()

	srv := newServerWithManager(mgr)
	args := json.RawMessage(`{"app":"demo","namespace":"apps","patch":"{\"spec\":{\"replicas\":9}}","clusters":["cA","cB"]}`)
	res, err := srv.handlePatchApp(context.Background(), args)
	if err != nil {
		t.Fatalf("handlePatchApp: %v", err)
	}
	if got := executeResultsCount(t, res); got != 2 {
		t.Fatalf("results = %d, want 2", got)
	}
	m := res.(map[string]interface{})
	if m["app"].(string) != "demo" {
		t.Fatalf("app = %v", m["app"])
	}
}

// TestHandlePatchApp_MergePatchType covers the patch_type="merge" branch.
func TestHandlePatchApp_MergePatchType(t *testing.T) {
	mgr, cleanup := managerWithAppsServers(t, map[string]findAppFixtures{
		"cA": {deployments: []appsv1.Deployment{mkDeployment("demo", "apps", "demo", 1, 1)}},
	})
	defer cleanup()

	srv := newServerWithManager(mgr)
	args := json.RawMessage(`{"app":"demo","namespace":"apps","patch":"{\"spec\":{\"replicas\":5}}","patch_type":"merge","clusters":["cA"]}`)
	res, err := srv.handlePatchApp(context.Background(), args)
	if err != nil {
		t.Fatalf("handlePatchApp: %v", err)
	}
	if got := executeResultsCount(t, res); got != 1 {
		t.Fatalf("results = %d, want 1", got)
	}
}

// TestHandlePatchApp_JSONPatchType covers the patch_type="json" branch.
func TestHandlePatchApp_JSONPatchType(t *testing.T) {
	mgr, cleanup := managerWithAppsServers(t, map[string]findAppFixtures{
		"cA": {deployments: []appsv1.Deployment{mkDeployment("demo", "apps", "demo", 1, 1)}},
	})
	defer cleanup()

	srv := newServerWithManager(mgr)
	args := json.RawMessage(`{"app":"demo","namespace":"apps","patch":"[{\"op\":\"replace\",\"path\":\"/spec/replicas\",\"value\":4}]","patch_type":"json","clusters":["cA"]}`)
	res, err := srv.handlePatchApp(context.Background(), args)
	if err != nil {
		t.Fatalf("handlePatchApp: %v", err)
	}
	if got := executeResultsCount(t, res); got != 1 {
		t.Fatalf("results = %d, want 1", got)
	}
}

// TestHandlePatchApp_DiscoverClustersFallback exercises the empty-Clusters
// branch that calls manager.DiscoverClusters() to build the target list.
func TestHandlePatchApp_DiscoverClustersFallback(t *testing.T) {
	mgr, cleanup := managerWithAppsServers(t, map[string]findAppFixtures{
		"only": {deployments: []appsv1.Deployment{mkDeployment("demo", "apps", "demo", 1, 1)}},
	})
	defer cleanup()

	srv := newServerWithManager(mgr)
	args := json.RawMessage(`{"app":"demo","namespace":"apps","patch":"{\"spec\":{\"replicas\":2}}"}`)
	res, err := srv.handlePatchApp(context.Background(), args)
	if err != nil {
		t.Fatalf("handlePatchApp: %v", err)
	}
	if got := executeResultsCount(t, res); got != 1 {
		t.Fatalf("results = %d, want 1", got)
	}
}
