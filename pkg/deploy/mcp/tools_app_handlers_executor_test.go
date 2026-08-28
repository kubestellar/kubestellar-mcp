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
	App       string        `json:"app"`
	Instances []AppInstance `json:"instances"`
	Count     int           `json:"count"`
} {
	t.Helper()
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out struct {
		App       string        `json:"app"`
		Instances []AppInstance `json:"instances"`
		Count     int           `json:"count"`
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

// A cluster whose apiserver returns 500 must not error the whole call —
// findAppInCluster swallows list errors so we get an empty instances slice.
func TestHandleGetAppInstances_BrokenClusterYieldsEmpty(t *testing.T) {
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
