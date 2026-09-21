package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
)

// decodeAppInstancesResult round-trips the map[string]interface{} returned by
// handleGetAppInstances into a typed struct.
func decodeAppInstancesResult(t *testing.T, res interface{}) struct {
	App               string        `json:"app"`
	Instances         []AppInstance `json:"instances"`
	Count             int           `json:"count"`
	UncheckedClusters []string      `json:"uncheckedClusters,omitempty"`
} {
	t.Helper()
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out struct {
		App               string        `json:"app"`
		Instances         []AppInstance `json:"instances"`
		Count             int           `json:"count"`
		UncheckedClusters []string      `json:"uncheckedClusters,omitempty"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func TestHandleGetAppInstances_MalformedJSON(t *testing.T) {
	srv := &Server{}
	if _, err := srv.handleGetAppInstances(context.Background(), json.RawMessage(`{`)); err == nil {
		t.Fatal("expected invalid-arguments error")
	}
}

// TestHandleGetAppInstances_ExecutorSuccess exercises the happy-path
// executor.Execute callback and the []AppInstance flatten loop in
// handleGetAppInstances, which the pure validation-error tests can't reach
// because they short-circuit before the executor runs.
func TestHandleGetAppInstances_ExecutorSuccess(t *testing.T) {
	mgr, cleanup := managerWithAppsServers(t, map[string]findAppFixtures{
		"cA": {
			deployments: []appsv1.Deployment{
				mkDeployment("demo-web", "app", "demo", 3, 3),
				mkDeployment("other", "app", "somethingelse", 1, 1), // no match
			},
			statefulsets: []appsv1.StatefulSet{
				mkStatefulSet("demo-db", "app", "demo", 2, 2),
			},
		},
		"cB": {
			deployments: []appsv1.Deployment{
				mkDeployment("demo-api", "app", "demo", 2, 2),
			},
		},
	})
	defer cleanup()

	srv := newServerWithManager(mgr)
	res, err := srv.handleGetAppInstances(context.Background(), json.RawMessage(`{"app":"demo"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := decodeAppInstancesResult(t, res)
	if out.App != "demo" {
		t.Fatalf("App = %q, want %q", out.App, "demo")
	}
	// 2 deployments + 1 statefulset = 3 instances across cA+cB
	if out.Count != 3 || len(out.Instances) != 3 {
		t.Fatalf("Count = %d, len(Instances) = %d, want 3/3: %+v",
			out.Count, len(out.Instances), out.Instances)
	}
	// Every returned instance must actually be labeled with app=demo.
	for _, inst := range out.Instances {
		if !strings.HasPrefix(inst.Name, "demo-") {
			t.Fatalf("instance %+v does not look like a demo-* app", inst)
		}
		if inst.Cluster != "cA" && inst.Cluster != "cB" {
			t.Fatalf("instance cluster = %q, want cA|cB", inst.Cluster)
		}
	}
}

// A cluster whose apiserver returns 500 must not error the whole call, but
// it also must not be silently indistinguishable from "app not deployed
// anywhere": findAppInCluster returns the List error, and
// handleGetAppInstances must surface that cluster in uncheckedClusters
// rather than just dropping it, so callers can tell a genuine zero-instance
// result apart from "some clusters couldn't be checked".
func TestHandleGetAppInstances_BrokenClusterYieldsUnchecked(t *testing.T) {
	mgr, cleanup := managerBadServer(t, "broken")
	defer cleanup()

	srv := newServerWithManager(mgr)
	res, err := srv.handleGetAppInstances(context.Background(), json.RawMessage(`{"app":"demo"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := decodeAppInstancesResult(t, res)
	if out.Count != 0 || len(out.Instances) != 0 {
		t.Fatalf("Count/Instances = %d/%d, want 0/0", out.Count, len(out.Instances))
	}
	if len(out.UncheckedClusters) != 1 || out.UncheckedClusters[0] != "broken" {
		t.Fatalf("UncheckedClusters = %v, want [broken]", out.UncheckedClusters)
	}
}

// A mix of a healthy cluster and a broken one must report the healthy
// cluster's real instances while still flagging the broken cluster as
// unchecked, rather than letting the broken cluster silently vanish.
func TestHandleGetAppInstances_MixedClustersReportsHealthyPlusUnchecked(t *testing.T) {
	mgr, cleanup := managerWithAppsServersAndBadCluster(t, map[string]findAppFixtures{
		"cGood": {
			deployments: []appsv1.Deployment{
				mkDeployment("demo-web", "app", "demo", 3, 3),
			},
		},
	}, "cBroken")
	defer cleanup()

	srv := newServerWithManager(mgr)
	res, err := srv.handleGetAppInstances(context.Background(), json.RawMessage(`{"app":"demo"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := decodeAppInstancesResult(t, res)
	if out.Count != 1 || len(out.Instances) != 1 {
		t.Fatalf("Count/Instances = %d/%d, want 1/1: %+v", out.Count, len(out.Instances), out.Instances)
	}
	if len(out.UncheckedClusters) != 1 || out.UncheckedClusters[0] != "cBroken" {
		t.Fatalf("UncheckedClusters = %v, want [cBroken]", out.UncheckedClusters)
	}
}

// handleGetAppLogs shares the same validation-and-executor skeleton as
// handleGetAppInstances/handleGetAppStatus. Covers the malformed-JSON branch
// missing from the existing tests, which only assert namespace/app-name
// validation errors.
func TestHandleGetAppLogs_MalformedJSON(t *testing.T) {
	srv := &Server{}
	if _, err := srv.handleGetAppLogs(context.Background(), json.RawMessage(`{`)); err == nil {
		t.Fatal("expected invalid-arguments error")
	}
}
