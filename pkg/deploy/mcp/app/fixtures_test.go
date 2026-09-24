package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func mustMarshalJSON(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return data
}

func decodeAppStatusResult(t *testing.T, res interface{}) AppStatus {
	t.Helper()
	data, err := json.Marshal(res)
	requireNoError(t, err)
	var out AppStatus
	requireNoError(t, json.Unmarshal(data, &out))
	return out
}

func decodeAppInstancesResult(t *testing.T, res interface{}) struct {
	App               string        `json:"app"`
	Instances         []AppInstance `json:"instances"`
	Count             int           `json:"count"`
	UncheckedClusters []string      `json:"uncheckedClusters,omitempty"`
} {
	t.Helper()
	data, err := json.Marshal(res)
	requireNoError(t, err)
	var out struct {
		App               string        `json:"app"`
		Instances         []AppInstance `json:"instances"`
		Count             int           `json:"count"`
		UncheckedClusters []string      `json:"uncheckedClusters,omitempty"`
	}
	requireNoError(t, json.Unmarshal(data, &out))
	return out
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func int32Ptr(i int32) *int32 { return &i }

func clientForServer(t *testing.T, srv *httptest.Server) *kubernetes.Clientset {
	t.Helper()
	client, err := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})
	if err != nil {
		t.Fatalf("kubernetes.NewForConfig: %v", err)
	}
	return client
}

func mkDeployment(name, ns, appLabel string, wantReplicas, ready int32) appsv1.Deployment {
	replicas := wantReplicas
	labels := map[string]string{}
	if appLabel != "" {
		labels["app"] = appLabel
	}
	return appsv1.Deployment{
		TypeMeta:   metav1.TypeMeta{Kind: "Deployment", APIVersion: "apps/v1"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: labels},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status:     appsv1.DeploymentStatus{ReadyReplicas: ready},
	}
}

func mkStatefulSet(name, ns, appLabel string, wantReplicas, ready int32) appsv1.StatefulSet {
	replicas := wantReplicas
	labels := map[string]string{}
	if appLabel != "" {
		labels["app.kubernetes.io/name"] = appLabel
	}
	return appsv1.StatefulSet{
		TypeMeta:   metav1.TypeMeta{Kind: "StatefulSet", APIVersion: "apps/v1"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: labels},
		Spec:       appsv1.StatefulSetSpec{Replicas: &replicas},
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

type findAppFixtures struct {
	deployments  []appsv1.Deployment
	statefulsets []appsv1.StatefulSet
	daemonsets   []appsv1.DaemonSet
}

func startAppsServer(t *testing.T, fx findAppFixtures) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case strings.HasSuffix(path, "/deployments"):
			_ = json.NewEncoder(w).Encode(&appsv1.DeploymentList{
				TypeMeta: metav1.TypeMeta{Kind: "DeploymentList", APIVersion: "apps/v1"},
				Items:    fx.deployments,
			})
		case strings.HasSuffix(path, "/statefulsets"):
			_ = json.NewEncoder(w).Encode(&appsv1.StatefulSetList{
				TypeMeta: metav1.TypeMeta{Kind: "StatefulSetList", APIVersion: "apps/v1"},
				Items:    fx.statefulsets,
			})
		case strings.HasSuffix(path, "/daemonsets"):
			_ = json.NewEncoder(w).Encode(&appsv1.DaemonSetList{
				TypeMeta: metav1.TypeMeta{Kind: "DaemonSetList", APIVersion: "apps/v1"},
				Items:    fx.daemonsets,
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

func mkPod(name, ns, appLabel string, containers ...string) corev1.Pod {
	out := make([]corev1.Container, 0, len(containers))
	for _, container := range containers {
		out = append(out, corev1.Container{Name: container})
	}
	labels := map[string]string{}
	if appLabel != "" {
		labels["app"] = appLabel
	}
	return corev1.Pod{
		TypeMeta:   metav1.TypeMeta{Kind: "Pod", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: labels},
		Spec:       corev1.PodSpec{Containers: out},
	}
}

func startPodsAndLogsServer(t *testing.T, pods []corev1.Pod, logLines map[string]map[string][]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasPrefix(path, "/api/v1/namespaces/") && strings.HasSuffix(path, "/log") {
			parts := strings.Split(strings.TrimPrefix(path, "/api/v1/namespaces/"), "/")
			if len(parts) < 4 {
				http.NotFound(w, r)
				return
			}
			podName := parts[2]
			container := r.URL.Query().Get("container")
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			for _, line := range logLines[podName][container] {
				_, _ = fmt.Fprintln(w, line)
			}
			return
		}
		if strings.HasSuffix(path, "/pods") {
			w.Header().Set("Content-Type", "application/json")
			filtered := pods
			if strings.HasPrefix(path, "/api/v1/namespaces/") {
				filtered = nil
				parts := strings.Split(strings.TrimPrefix(path, "/api/v1/namespaces/"), "/")
				if len(parts) >= 2 {
					ns := parts[0]
					for _, pod := range pods {
						if pod.Namespace == ns {
							filtered = append(filtered, pod)
						}
					}
				}
			}
			_ = json.NewEncoder(w).Encode(&corev1.PodList{TypeMeta: metav1.TypeMeta{Kind: "PodList", APIVersion: "v1"}, Items: filtered})
			return
		}
		http.NotFound(w, r)
	}))
}

type fakeExecutor struct {
	order   []string
	clients map[string]*kubernetes.Clientset
	errs    map[string]error
	calls   []string
	execErr error
}

func (e *fakeExecutor) Execute(ctx context.Context, clusterName string, fn multicluster.ExecuteFunc) ([]multicluster.ClusterResult, error) {
	if e.execErr != nil {
		return nil, e.execErr
	}
	names := e.order
	if clusterName != "" {
		names = []string{clusterName}
	}
	results := make([]multicluster.ClusterResult, 0, len(names))
	for _, name := range names {
		e.calls = append(e.calls, name)
		if err := e.errs[name]; err != nil {
			results = append(results, multicluster.ClusterResult{Cluster: name, Error: err.Error()})
			continue
		}
		client := e.clients[name]
		res, err := fn(ctx, client, name)
		if err != nil {
			results = append(results, multicluster.ClusterResult{Cluster: name, Error: err.Error()})
			continue
		}
		results = append(results, multicluster.ClusterResult{Cluster: name, Result: res})
	}
	return results, nil
}
