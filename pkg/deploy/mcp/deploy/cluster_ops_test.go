package deploy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestScaleAppInCluster_UpdatesReplicasAndReturnsOld(t *testing.T) {
	fx := findAppFixtures{deployments: []appsv1.Deployment{mkDeployment("demo-web", "default", "demo", 2, 2)}}
	updated := map[string]*appsv1.Deployment{}
	server := startAppsServer(t, fx, updated)
	defer server.Close()

	res, err := ScaleAppInCluster(context.Background(), clientForServer(t, server), "cA", "demo", "", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := res.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", res)
	}
	if m["deployment"] != "demo-web" || m["oldReplicas"] != int32(2) || m["newReplicas"] != int32(5) || m["cluster"] != "cA" {
		t.Fatalf("unexpected result: %+v", m)
	}
	if updated["demo-web"] == nil {
		t.Fatal("scale did not record PUT to demo-web")
	}
}

func TestScaleAppInCluster_NilSpecReplicasDefaultsToOne(t *testing.T) {
	dep := mkDeployment("demo", "default", "demo", 0, 0)
	dep.Spec.Replicas = nil
	fx := findAppFixtures{deployments: []appsv1.Deployment{dep}}
	server := startAppsServer(t, fx, map[string]*appsv1.Deployment{})
	defer server.Close()

	res, err := ScaleAppInCluster(context.Background(), clientForServer(t, server), "cA", "demo", "default", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := res.(map[string]interface{})
	if m["oldReplicas"] != int32(1) {
		t.Fatalf("oldReplicas = %v, want 1", m["oldReplicas"])
	}
}

func TestScaleAppInCluster_NotFound(t *testing.T) {
	fx := findAppFixtures{deployments: []appsv1.Deployment{mkDeployment("other", "default", "other", 1, 1)}}
	server := startAppsServer(t, fx, nil)
	defer server.Close()

	_, err := ScaleAppInCluster(context.Background(), clientForServer(t, server), "cA", "demo", "", 3)
	if err == nil {
		t.Fatal("expected not-found error, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error should mention not-found, got: %v", err)
	}
}

func TestScaleAppInCluster_ListError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	if _, err := ScaleAppInCluster(context.Background(), clientForServer(t, server), "cA", "demo", "", 3); err == nil {
		t.Fatal("expected list error")
	}
}

func TestPatchAppInCluster_PatchesFirstMatch(t *testing.T) {
	fx := findAppFixtures{deployments: []appsv1.Deployment{mkDeployment("demo-web", "default", "demo", 2, 2)}}
	updated := map[string]*appsv1.Deployment{}
	server := startAppsServer(t, fx, updated)
	defer server.Close()

	res, err := PatchAppInCluster(context.Background(), clientForServer(t, server), "cA", "demo", "", []byte(`{"spec":{"replicas":9}}`), types.MergePatchType)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := res.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", res)
	}
	if m["cluster"] != "cA" || m["deployment"] != "demo-web" || m["status"] != "patched" {
		t.Fatalf("unexpected result: %+v", m)
	}
	if updated["demo-web"] == nil {
		t.Fatalf("patch body was not received by server: %+v", updated)
	}
}

func TestPatchAppInCluster_NotFound(t *testing.T) {
	fx := findAppFixtures{deployments: []appsv1.Deployment{mkDeployment("other", "default", "other", 1, 1)}}
	server := startAppsServer(t, fx, nil)
	defer server.Close()

	if _, err := PatchAppInCluster(context.Background(), clientForServer(t, server), "cA", "demo", "", []byte(`{}`), types.MergePatchType); err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestPatchAppInCluster_ListError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	if _, err := PatchAppInCluster(context.Background(), clientForServer(t, server), "cA", "demo", "app", []byte(`{}`), types.MergePatchType); err == nil {
		t.Fatal("expected list error")
	}
}

func TestBoolPtr(t *testing.T) {
	if got := BoolPtr(true); got == nil || !*got {
		t.Fatalf("BoolPtr(true) = %v, want pointer to true", got)
	}
	if got := BoolPtr(false); got == nil || *got {
		t.Fatalf("BoolPtr(false) = %v, want pointer to false", got)
	}
}
