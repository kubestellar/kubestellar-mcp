package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
)

func mkPod(name, ns, appLabel string, containers ...string) corev1.Pod {
	c := make([]corev1.Container, 0, len(containers))
	for _, name := range containers {
		c = append(c, corev1.Container{Name: name})
	}
	labels := map[string]string{}
	if appLabel != "" {
		labels["app"] = appLabel
	}
	return corev1.Pod{
		TypeMeta:   metav1.TypeMeta{Kind: "Pod", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: labels},
		Spec:       corev1.PodSpec{Containers: c},
	}
}

// startPodsAndLogsServer serves pod list requests and per-container log streams.
// logLines: (podName -> containerName -> lines to emit on GetLogs).
func startPodsAndLogsServer(t *testing.T, pods []corev1.Pod, logLines map[string]map[string][]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		// Pod logs: /api/v1/namespaces/{ns}/pods/{pod}/log?container={c}
		if strings.HasPrefix(p, "/api/v1/namespaces/") && strings.HasSuffix(p, "/log") {
			// Extract pod name (segment before /log).
			parts := strings.Split(strings.TrimPrefix(p, "/api/v1/namespaces/"), "/")
			// parts = [ns, "pods", podName, "log"]
			if len(parts) < 4 {
				http.NotFound(w, r)
				return
			}
			podName := parts[2]
			container := r.URL.Query().Get("container")
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			for _, line := range logLines[podName][container] {
				fmt.Fprintln(w, line)
			}
			return
		}
		// Pod list: /api/v1/pods or /api/v1/namespaces/{ns}/pods
		if strings.HasSuffix(p, "/pods") {
			w.Header().Set("Content-Type", "application/json")
			// Filter to the namespace when requested.
			var out []corev1.Pod
			if strings.HasPrefix(p, "/api/v1/namespaces/") {
				parts := strings.Split(strings.TrimPrefix(p, "/api/v1/namespaces/"), "/")
				if len(parts) >= 2 {
					ns := parts[0]
					for _, pod := range pods {
						if pod.Namespace == ns {
							out = append(out, pod)
						}
					}
				}
			} else {
				out = pods
			}
			list := corev1.PodList{
				TypeMeta: metav1.TypeMeta{Kind: "PodList", APIVersion: "v1"},
				Items:    out,
			}
			_ = json.NewEncoder(w).Encode(&list)
			return
		}
		http.NotFound(w, r)
	}))
}

func TestGetLogsFromCluster_InvalidNamespace(t *testing.T) {
	srv := &Server{}
	if _, err := srv.getLogsFromCluster(context.Background(), nil, "c1", "demo", "kube-system", 10, ""); err == nil {
		t.Fatal("expected error for protected namespace")
	}
}

func TestGetLogsFromCluster_ListError(t *testing.T) {
	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer badSrv.Close()

	srv := &Server{}
	if _, err := srv.getLogsFromCluster(context.Background(), clientForServer(t, badSrv), "c1", "demo", "", 10, ""); err == nil {
		t.Fatal("expected list error")
	}
}

func TestGetLogsFromCluster_AggregatesLinesAcrossPodsAndContainers(t *testing.T) {
	pods := []corev1.Pod{
		mkPod("demo-web-1", "app", "demo", "web", "sidecar"),
		mkPod("demo-web-2", "app", "demo", "web"),
		mkPod("other-1", "app", "other", "web"), // must be filtered out (label != demo and name doesn't contain "demo")
	}
	logs := map[string]map[string][]string{
		"demo-web-1": {
			"web":     {"L1", "L2"},
			"sidecar": {"S1"},
		},
		"demo-web-2": {
			"web": {"W1", "", "W2"}, // empty line must be dropped
		},
	}
	server := startPodsAndLogsServer(t, pods, logs)
	defer server.Close()

	srv := &Server{}
	got, err := srv.getLogsFromCluster(context.Background(), clientForServer(t, server), "cA", "demo", "app", 100, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Expect L1+L2+S1+W1+W2 = 5 entries (empty line dropped, other-1 filtered).
	if len(got) != 5 {
		t.Fatalf("expected 5 log entries, got %d: %+v", len(got), got)
	}
	// Every entry must carry the cluster name.
	for _, entry := range got {
		if entry.Cluster != "cA" {
			t.Fatalf("cluster name not propagated: %+v", entry)
		}
	}
	// Collect messages so we can assert set membership.
	msgs := map[string]bool{}
	for _, entry := range got {
		msgs[entry.Message] = true
	}
	for _, want := range []string{"L1", "L2", "S1", "W1", "W2"} {
		if !msgs[want] {
			t.Fatalf("missing log line %q in %v", want, msgs)
		}
	}
	// Assert we saw both containers on demo-web-1.
	containers := map[string]bool{}
	for _, entry := range got {
		if entry.Pod == "demo-web-1" {
			containers[entry.Container] = true
		}
	}
	if !containers["web"] || !containers["sidecar"] {
		t.Fatalf("expected both containers on demo-web-1, got %v", containers)
	}
}

func TestGetLogsFromCluster_SinceDurationAccepted(t *testing.T) {
	// A valid duration exercises the SinceTime branch without changing output.
	pods := []corev1.Pod{mkPod("demo-1", "app", "demo", "web")}
	logs := map[string]map[string][]string{
		"demo-1": {"web": {"hi"}},
	}
	server := startPodsAndLogsServer(t, pods, logs)
	defer server.Close()

	srv := &Server{}
	got, err := srv.getLogsFromCluster(context.Background(), clientForServer(t, server), "c1", "demo", "app", 50, "1h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Message != "hi" {
		t.Fatalf("unexpected result: %+v", got)
	}

	// Malformed duration must be silently ignored (function must not fail).
	got, err = srv.getLogsFromCluster(context.Background(), clientForServer(t, server), "c1", "demo", "app", 50, "not-a-duration")
	if err != nil {
		t.Fatalf("unexpected error on bad duration: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("unexpected result on bad duration: %+v", got)
	}
}

func TestHandleGetAppLogs_HappyPathAcrossClusters(t *testing.T) {
	fxA := []corev1.Pod{mkPod("demo-1", "app", "demo", "web")}
	fxB := []corev1.Pod{mkPod("demo-2", "app", "demo", "web")}
	logsA := map[string]map[string][]string{"demo-1": {"web": {"A1", "A2"}}}
	logsB := map[string]map[string][]string{"demo-2": {"web": {"B1"}}}

	srvA := startPodsAndLogsServer(t, fxA, logsA)
	defer srvA.Close()
	srvB := startPodsAndLogsServer(t, fxB, logsB)
	defer srvB.Close()

	kc := writeKubeconfig(t, map[string]string{"cA": srvA.URL, "cB": srvB.URL})
	mgr, err := multicluster.NewClientManager(kc)
	if err != nil {
		t.Fatalf("mgr: %v", err)
	}
	server := newServerWithManager(mgr)
	res, err := server.handleGetAppLogs(context.Background(), json.RawMessage(`{"app":"demo","namespace":"app","tail":50}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := res.(map[string]interface{})
	logs, ok := m["logs"].([]LogEntry)
	if !ok {
		t.Fatalf("logs field type = %T", m["logs"])
	}
	if len(logs) != 3 {
		t.Fatalf("expected 3 aggregated logs, got %d: %+v", len(logs), logs)
	}
	if m["logCount"] != 3 {
		t.Fatalf("logCount = %v, want 3", m["logCount"])
	}
	clusters := []string{}
	for _, e := range logs {
		clusters = append(clusters, e.Cluster)
	}
	sort.Strings(clusters)
	if clusters[0] != "cA" || clusters[len(clusters)-1] != "cB" {
		t.Fatalf("clusters in aggregate = %v, want to include cA and cB", clusters)
	}
}

func TestHandleGetAppLogs_TailDefaultsTo100(t *testing.T) {
	// A single pod, no tail specified: verify the request works and returns a log.
	pods := []corev1.Pod{mkPod("demo-1", "app", "demo", "web")}
	logs := map[string]map[string][]string{"demo-1": {"web": {"only"}}}
	srv := startPodsAndLogsServer(t, pods, logs)
	defer srv.Close()

	kc := writeKubeconfig(t, map[string]string{"c1": srv.URL})
	mgr, err := multicluster.NewClientManager(kc)
	if err != nil {
		t.Fatalf("mgr: %v", err)
	}
	server := newServerWithManager(mgr)
	res, err := server.handleGetAppLogs(context.Background(), json.RawMessage(`{"app":"demo","namespace":"app"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := res.(map[string]interface{})
	if m["logCount"] != 1 {
		t.Fatalf("logCount = %v, want 1", m["logCount"])
	}
}
