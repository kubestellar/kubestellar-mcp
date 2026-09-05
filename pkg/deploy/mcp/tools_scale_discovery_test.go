package mcp

import (
	"context"
	"encoding/json"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
)

// This file targets the last-few uncovered branches of handleScaleApp
// (tools_deploy.go:423) that existing tools_scale_uncovered_test.go and
// tools_deploy_test.go skip. Focus is on the auto-cluster-discovery path
// (params.Clusters is empty) — the fan-out through handleGetAppInstances
// and the "not found in any cluster" error arm.

// TestHandleScaleApp_NoClustersAndNoInstancesReturnsNotFoundError exercises
// tools_deploy.go:465-466 — the guard that returns "app %s not found in
// any cluster" when both the caller-supplied cluster list AND the instance
// discovery fallback yield no target clusters. Prior tests always supply
// a clusters argument, leaving this branch uncovered.
func TestHandleScaleApp_NoClustersAndNoInstancesReturnsNotFoundError(t *testing.T) {
	// Manager with a real cluster that has NO deployments matching the
	// requested app — handleGetAppInstances returns an empty instance list,
	// so targetClusters remains empty and the not-found guard fires.
	mgr, cleanup := managerWithAppsServers(t, map[string]findAppFixtures{
		"only": {deployments: []appsv1.Deployment{mkDeployment("other", "apps", "other", 1, 1)}},
	})
	defer cleanup()

	srv := newServerWithManager(mgr)
	args := json.RawMessage(`{"app":"missing-app","namespace":"apps","replicas":3}`)

	_, err := srv.handleScaleApp(context.Background(), args)
	if err == nil {
		t.Fatal("expected 'not found in any cluster' error, got nil")
	}
	if got := err.Error(); got == "" ||
		(!contains(got, "missing-app") || !contains(got, "not found in any cluster")) {
		t.Fatalf("err = %q, want message mentioning app name and 'not found in any cluster'", got)
	}
}

// TestHandleScaleApp_AutoDiscoversClustersFromInstances exercises the
// happy-path fallback branch at tools_deploy.go:449-454 — when
// params.Clusters is empty, handleGetAppInstances returns real instances
// and their cluster names are added to targetClusters. This runs a full
// scale end-to-end through the executor.
func TestHandleScaleApp_AutoDiscoversClustersFromInstances(t *testing.T) {
	mgr, cleanup := managerWithAppsServers(t, map[string]findAppFixtures{
		"discovered-cluster": {
			deployments: []appsv1.Deployment{mkDeployment("demo", "apps", "demo", 1, 1)},
		},
	})
	defer cleanup()

	srv := newServerWithManager(mgr)
	// NOTE: no "clusters" field -> forces the discovery branch.
	args := json.RawMessage(`{"app":"demo","namespace":"apps","replicas":4}`)

	res, err := srv.handleScaleApp(context.Background(), args)
	if err != nil {
		t.Fatalf("handleScaleApp: %v", err)
	}
	m, ok := res.(map[string]interface{})
	if !ok {
		t.Fatalf("res = %#v, want map", res)
	}
	if m["app"].(string) != "demo" {
		t.Fatalf("app = %v, want demo", m["app"])
	}
	if reps, ok := m["replicas"].(int32); !ok || reps != 4 {
		t.Fatalf("replicas = %v, want int32(4)", m["replicas"])
	}
	// results is a []multicluster.ClusterResult of the discovered cluster.
	if got := executeResultsCount(t, res); got != 1 {
		t.Fatalf("results = %d, want 1 (auto-discovered cluster)", got)
	}
}

// contains is a tiny substring helper to avoid a strings import in
// this one file when the assertion is a simple contains check.
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
