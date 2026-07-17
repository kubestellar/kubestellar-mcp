package mcp

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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

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

func mkStatefulSet(name, ns, appLabel string, wantReplicas, ready int32) appsv1.StatefulSet {
	r := wantReplicas
	labels := map[string]string{}
	if appLabel != "" {
		labels["app.kubernetes.io/name"] = appLabel
	}
	return appsv1.StatefulSet{
		TypeMeta:   metav1.TypeMeta{Kind: "StatefulSet", APIVersion: "apps/v1"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: labels},
		Spec:       appsv1.StatefulSetSpec{Replicas: &r},
		Status:     appsv1.StatefulSetStatus{ReadyReplicas: ready},
	}
}

func mkDaemonSet(name, ns, appLabel string, desired, ready int32) appsv1.DaemonSet {
	labels := map[string]string{}
	if appLabel != "" {
		labels["app"] = appLabel
	}
	return appsv1.DaemonSet{
		TypeMeta:   metav1.TypeMeta{Kind: "DaemonSet", APIVersion: "apps/v1"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: labels},
		Status: appsv1.DaemonSetStatus{
			DesiredNumberScheduled: desired,
			NumberReady:            ready,
		},
	}
}

// findAppFixtures holds items served by the fake apiserver for list ops.
type findAppFixtures struct {
	deployments  []appsv1.Deployment
	statefulsets []appsv1.StatefulSet
	daemonsets   []appsv1.DaemonSet
}

// startAppsServer serves list requests for deployments/statefulsets/daemonsets
// (both cluster-scoped "all namespaces" and namespaced paths) plus a
// per-deployment PUT/PATCH handler used by scale/patch tests. Any deployment
// name in updated must match a name in fx.deployments to succeed.
func startAppsServer(t *testing.T, fx findAppFixtures, updated map[string]*appsv1.Deployment) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path

		// Individual deployment PUT / PATCH
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
				// The client sends application/vnd.kubernetes.protobuf, so we
				// don't attempt to decode. Return a minimal Deployment JSON
				// with the URL-derived name.
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
			// PATCH: just record and echo back a stub deployment with the name.
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

func TestFindAppInCluster_InvalidNamespace(t *testing.T) {
	srv := &Server{}
	if _, err := srv.findAppInCluster(context.Background(), nil, "c1", "demo", "kube-system"); err == nil {
		t.Fatal("expected error for protected namespace, got nil")
	}
}

func TestFindAppInCluster_MatchesAcrossKinds(t *testing.T) {
	fx := findAppFixtures{
		deployments: []appsv1.Deployment{
			mkDeployment("demo-web", "app", "demo", 3, 3),         // matches app label
			mkDeployment("unrelated", "other", "somethingelse", 1, 1), // no match
		},
		statefulsets: []appsv1.StatefulSet{
			mkStatefulSet("demo-db", "app", "demo", 2, 1), // degraded
		},
		daemonsets: []appsv1.DaemonSet{
			mkDaemonSet("demo-daemon", "app", "demo", 3, 0), // failed
		},
	}
	server := startAppsServer(t, fx, nil)
	defer server.Close()

	srv := &Server{}
	got, err := srv.findAppInCluster(context.Background(), clientForServer(t, server), "cA", "demo", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Expect 3 matches (deploy demo-web, sts demo-db, ds demo-daemon).
	if len(got) != 3 {
		t.Fatalf("expected 3 instances, got %d: %+v", len(got), got)
	}

	byKind := map[string]AppInstance{}
	for _, inst := range got {
		byKind[inst.Kind] = inst
	}

	if d, ok := byKind["Deployment"]; !ok || d.Name != "demo-web" || d.Status != "healthy" || d.Replicas != 3 {
		t.Fatalf("deployment entry unexpected: %+v", d)
	}
	if s, ok := byKind["StatefulSet"]; !ok || s.Name != "demo-db" || s.Status != "degraded" || s.Replicas != 2 || s.ReadyReplicas != 1 {
		t.Fatalf("statefulset entry unexpected: %+v", s)
	}
	if d, ok := byKind["DaemonSet"]; !ok || d.Name != "demo-daemon" || d.Status != "failed" || d.Replicas != 3 || d.ReadyReplicas != 0 {
		t.Fatalf("daemonset entry unexpected: %+v", d)
	}
	for _, inst := range got {
		if inst.Cluster != "cA" {
			t.Fatalf("cluster name not propagated: %+v", inst)
		}
	}
}

func TestFindAppInCluster_NoMatches(t *testing.T) {
	fx := findAppFixtures{
		deployments: []appsv1.Deployment{mkDeployment("other", "app", "other", 1, 1)},
	}
	server := startAppsServer(t, fx, nil)
	defer server.Close()

	srv := &Server{}
	got, err := srv.findAppInCluster(context.Background(), clientForServer(t, server), "c1", "demo", "app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no instances, got %+v", got)
	}
}

func TestScaleAppInCluster_UpdatesReplicasAndReturnsOld(t *testing.T) {
	fx := findAppFixtures{
		deployments: []appsv1.Deployment{mkDeployment("demo-web", "default", "demo", 2, 2)},
	}
	updated := map[string]*appsv1.Deployment{}
	server := startAppsServer(t, fx, updated)
	defer server.Close()

	srv := &Server{}
	res, err := srv.scaleAppInCluster(context.Background(), clientForServer(t, server), "cA", "demo", "", 5)
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
	d := updated["demo-web"]
	if d == nil {
		t.Fatal("scale did not record PUT to demo-web")
	}
	if d.Annotations["put-body-len"] == "" || d.Annotations["put-body-len"] == "0" {
		t.Fatalf("server did not receive PUT body: %+v", d.Annotations)
	}
}

func TestScaleAppInCluster_NilSpecReplicasDefaultsToOne(t *testing.T) {
	// Deployment with nil Spec.Replicas — the old-replicas value must default to 1.
	dep := mkDeployment("demo", "default", "demo", 0, 0)
	dep.Spec.Replicas = nil
	fx := findAppFixtures{deployments: []appsv1.Deployment{dep}}
	server := startAppsServer(t, fx, map[string]*appsv1.Deployment{})
	defer server.Close()

	srv := &Server{}
	res, err := srv.scaleAppInCluster(context.Background(), clientForServer(t, server), "cA", "demo", "default", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := res.(map[string]interface{})
	if m["oldReplicas"] != int32(1) {
		t.Fatalf("oldReplicas = %v, want 1", m["oldReplicas"])
	}
}

func TestScaleAppInCluster_NotFound(t *testing.T) {
	fx := findAppFixtures{
		deployments: []appsv1.Deployment{mkDeployment("other", "default", "other", 1, 1)},
	}
	server := startAppsServer(t, fx, nil)
	defer server.Close()

	srv := &Server{}
	_, err := srv.scaleAppInCluster(context.Background(), clientForServer(t, server), "cA", "demo", "", 3)
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

	srv := &Server{}
	if _, err := srv.scaleAppInCluster(context.Background(), clientForServer(t, server), "cA", "demo", "", 3); err == nil {
		t.Fatal("expected list error")
	}
}

func TestPatchAppInCluster_PatchesFirstMatch(t *testing.T) {
	fx := findAppFixtures{
		deployments: []appsv1.Deployment{mkDeployment("demo-web", "default", "demo", 2, 2)},
	}
	updated := map[string]*appsv1.Deployment{}
	server := startAppsServer(t, fx, updated)
	defer server.Close()

	srv := &Server{}
	res, err := srv.patchAppInCluster(context.Background(), clientForServer(t, server), "cA", "demo", "", []byte(`{"spec":{"replicas":9}}`), types.MergePatchType)
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
	d := updated["demo-web"]
	if d == nil || d.Annotations["patch"] == "" {
		t.Fatalf("patch body was not received by server: %+v", d)
	}
}

func TestPatchAppInCluster_NotFound(t *testing.T) {
	fx := findAppFixtures{
		deployments: []appsv1.Deployment{mkDeployment("other", "default", "other", 1, 1)},
	}
	server := startAppsServer(t, fx, nil)
	defer server.Close()

	srv := &Server{}
	if _, err := srv.patchAppInCluster(context.Background(), clientForServer(t, server), "cA", "demo", "", []byte(`{}`), types.MergePatchType); err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestPatchAppInCluster_ListError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	srv := &Server{}
	if _, err := srv.patchAppInCluster(context.Background(), clientForServer(t, server), "cA", "demo", "app", []byte(`{}`), types.MergePatchType); err == nil {
		t.Fatal("expected list error")
	}
}
