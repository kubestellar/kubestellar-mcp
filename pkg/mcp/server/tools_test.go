package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/cluster"
)

type stubDiscoverer struct {
	discoverClusters   func(source string) ([]cluster.ClusterInfo, error)
	checkHealthByCtxFn func(contextName string) (*cluster.HealthInfo, error)
}

func (s stubDiscoverer) DiscoverClusters(source string) ([]cluster.ClusterInfo, error) {
	if s.discoverClusters != nil {
		return s.discoverClusters(source)
	}
	return nil, nil
}

func (s stubDiscoverer) CheckHealthByContext(contextName string) (*cluster.HealthInfo, error) {
	if s.checkHealthByCtxFn != nil {
		return s.checkHealthByCtxFn(contextName)
	}
	return nil, nil
}

func TestHandleToolsCallDispatch(t *testing.T) {
	now := metav1.NewTime(time.Date(2024, time.January, 2, 3, 4, 5, 0, time.UTC))

	tests := []struct {
		name      string
		tool      string
		args      map[string]interface{}
		server    *Server
		wantError bool
		wantText  []string
	}{
		{
			name: "list clusters success",
			tool: "list_clusters",
			args: map[string]interface{}{"source": "all"},
			server: &Server{discoverer: stubDiscoverer{discoverClusters: func(source string) ([]cluster.ClusterInfo, error) {
				if source != "all" {
					t.Fatalf("DiscoverClusters source = %q, want all", source)
				}
				return []cluster.ClusterInfo{{Name: "alpha", Context: "alpha", Source: "kubeconfig", Server: "https://alpha", Current: true, Status: "Healthy"}}, nil
			}}},
			wantText: []string{"Discovered clusters:", "alpha (current)", "Status: Healthy"},
		},
		{
			name: "list clusters error",
			tool: "list_clusters",
			server: &Server{discoverer: stubDiscoverer{discoverClusters: func(source string) ([]cluster.ClusterInfo, error) {
				return nil, errors.New("discovery failed")
			}}},
			wantError: true,
			wantText:  []string{"Failed to discover clusters", "discovery failed"},
		},
		{
			name: "get cluster health success",
			tool: "get_cluster_health",
			args: map[string]interface{}{"cluster": "alpha"},
			server: &Server{discoverer: stubDiscoverer{
				discoverClusters: func(source string) ([]cluster.ClusterInfo, error) {
					return []cluster.ClusterInfo{{Name: "alpha", Context: "alpha", Server: "https://alpha"}}, nil
				},
				checkHealthByCtxFn: func(contextName string) (*cluster.HealthInfo, error) {
					if contextName != "alpha" {
						t.Fatalf("CheckHealthByContext context = %q, want alpha", contextName)
					}
					return &cluster.HealthInfo{Status: "Healthy", APIServerStatus: "Healthy", NodesReady: "2/2"}, nil
				},
			}},
			wantText: []string{"Cluster: alpha", "Status: Healthy", "Nodes Ready: 2/2"},
		},
		{
			name: "get cluster health error",
			tool: "get_cluster_health",
			args: map[string]interface{}{"cluster": "missing"},
			server: &Server{discoverer: stubDiscoverer{discoverClusters: func(source string) ([]cluster.ClusterInfo, error) {
				return []cluster.ClusterInfo{{Name: "alpha", Context: "alpha"}}, nil
			}}},
			wantError: true,
			wantText:  []string{"Cluster \"missing\" not found"},
		},
		{
			name: "get pods success",
			tool: "get_pods",
			args: map[string]interface{}{"namespace": "apps"},
			server: &Server{clientFactory: func(clusterName string) (kubernetes.Interface, error) {
				client := k8sfake.NewSimpleClientset(&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "apps"},
					Status: corev1.PodStatus{
						Phase:             corev1.PodRunning,
						StartTime:         &now,
						ContainerStatuses: []corev1.ContainerStatus{{Name: "main", Ready: true}},
					},
				})
				return client, nil
			}},
			wantText: []string{"Found 1 pods:", "apps/demo", "Running", "1/1"},
		},
		{
			name: "get pods error",
			tool: "get_pods",
			args: map[string]interface{}{"namespace": "apps"},
			server: &Server{clientFactory: func(clusterName string) (kubernetes.Interface, error) {
				client := k8sfake.NewSimpleClientset()
				client.PrependReactor("list", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("pod list failed")
				})
				return client, nil
			}},
			wantError: true,
			wantText:  []string{"Failed to list pods", "pod list failed"},
		},
		{
			name: "describe pod success",
			tool: "describe_pod",
			args: map[string]interface{}{"namespace": "apps", "name": "demo"},
			server: &Server{clientFactory: func(clusterName string) (kubernetes.Interface, error) {
				client := k8sfake.NewSimpleClientset(&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "apps"},
					Spec: corev1.PodSpec{
						NodeName:   "worker-1",
						Containers: []corev1.Container{{Name: "main", Image: "nginx:1.27"}},
					},
					Status: corev1.PodStatus{
						Phase:             corev1.PodRunning,
						PodIP:             "10.0.0.10",
						StartTime:         &now,
						ContainerStatuses: []corev1.ContainerStatus{{Name: "main", Ready: true, RestartCount: 1}},
						Conditions:        []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
					},
				})
				return client, nil
			}},
			wantText: []string{"Name: demo", "Node: worker-1", "main (image: nginx:1.27)", "main: ready, restarts: 1"},
		},
		{
			name:      "describe pod error",
			tool:      "describe_pod",
			args:      map[string]interface{}{"namespace": "apps"},
			server:    &Server{},
			wantError: true,
			wantText:  []string{"Pod name is required"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, rpcErr := callTool(t, tt.server, tt.tool, tt.args)
			if rpcErr != nil {
				t.Fatalf("handleToolsCall returned RPC error: %v", rpcErr)
			}
			if result.IsError != tt.wantError {
				t.Fatalf("IsError = %v, want %v", result.IsError, tt.wantError)
			}
			if len(result.Content) != 1 {
				t.Fatalf("content length = %d, want 1", len(result.Content))
			}
			for _, want := range tt.wantText {
				if !strings.Contains(result.Content[0].Text, want) {
					t.Fatalf("result text %q missing %q", result.Content[0].Text, want)
				}
			}
		})
	}
}

func TestHandleToolsCallUnknownTool(t *testing.T) {
	_, rpcErr := callTool(t, &Server{}, "missing_tool", nil)
	if rpcErr == nil {
		t.Fatal("expected RPC error for unknown tool")
	}
	if rpcErr.Code != -32602 || !strings.Contains(rpcErr.Message, "Unknown tool") {
		t.Fatalf("unexpected RPC error: %#v", rpcErr)
	}
}

func TestHandleToolsCallEmptyResultBranches(t *testing.T) {
	tests := []struct {
		name     string
		tool     string
		args     map[string]interface{}
		server   *Server
		wantText []string
	}{
		{
			name:     "list clusters no results",
			tool:     "list_clusters",
			args:     map[string]interface{}{"source": "all"},
			server:   &Server{discoverer: stubDiscoverer{discoverClusters: func(source string) ([]cluster.ClusterInfo, error) { return []cluster.ClusterInfo{}, nil }}},
			wantText: []string{"No clusters found"},
		},
		{
			name: "get cluster health without current context",
			tool: "get_cluster_health",
			server: &Server{discoverer: stubDiscoverer{discoverClusters: func(source string) ([]cluster.ClusterInfo, error) {
				return []cluster.ClusterInfo{{Name: "alpha", Context: "alpha", Current: false}}, nil
			}}},
			wantText: []string{"No current cluster context set"},
		},
		{
			name:     "get services empty",
			tool:     "get_services",
			server:   &Server{clientFactory: func(clusterName string) (kubernetes.Interface, error) { return k8sfake.NewSimpleClientset(), nil }},
			wantText: []string{"No services found"},
		},
		{
			name:     "get nodes empty",
			tool:     "get_nodes",
			server:   &Server{clientFactory: func(clusterName string) (kubernetes.Interface, error) { return k8sfake.NewSimpleClientset(), nil }},
			wantText: []string{"No nodes found"},
		},
		{
			name:     "get events empty",
			tool:     "get_events",
			server:   &Server{clientFactory: func(clusterName string) (kubernetes.Interface, error) { return k8sfake.NewSimpleClientset(), nil }},
			wantText: []string{"No events found"},
		},
		{
			name:     "get roles empty",
			tool:     "get_roles",
			server:   &Server{clientFactory: func(clusterName string) (kubernetes.Interface, error) { return k8sfake.NewSimpleClientset(), nil }},
			wantText: []string{"No roles found"},
		},
		{
			name:     "get cluster roles empty",
			tool:     "get_cluster_roles",
			server:   &Server{clientFactory: func(clusterName string) (kubernetes.Interface, error) { return k8sfake.NewSimpleClientset(), nil }},
			wantText: []string{"No cluster roles found"},
		},
		{
			name:     "get role bindings empty",
			tool:     "get_role_bindings",
			server:   &Server{clientFactory: func(clusterName string) (kubernetes.Interface, error) { return k8sfake.NewSimpleClientset(), nil }},
			wantText: []string{"No role bindings found"},
		},
		{
			name:     "get cluster role bindings empty",
			tool:     "get_cluster_role_bindings",
			server:   &Server{clientFactory: func(clusterName string) (kubernetes.Interface, error) { return k8sfake.NewSimpleClientset(), nil }},
			wantText: []string{"No cluster role bindings found"},
		},
		{
			name:     "check resource limits empty",
			tool:     "check_resource_limits",
			server:   &Server{clientFactory: func(clusterName string) (kubernetes.Interface, error) { return k8sfake.NewSimpleClientset(), nil }},
			wantText: []string{"All pods have resource limits configured"},
		},
		{
			name:     "check security issues empty",
			tool:     "check_security_issues",
			server:   &Server{clientFactory: func(clusterName string) (kubernetes.Interface, error) { return k8sfake.NewSimpleClientset(), nil }},
			wantText: []string{"No obvious security issues found"},
		},
		{
			name:     "warning events empty",
			tool:     "get_warning_events",
			server:   &Server{clientFactory: func(clusterName string) (kubernetes.Interface, error) { return k8sfake.NewSimpleClientset(), nil }},
			wantText: []string{"No warning events found"},
		},
		{
			name:     "helm releases empty",
			tool:     "check_helm_release_upgrades",
			server:   &Server{clientFactory: func(clusterName string) (kubernetes.Interface, error) { return k8sfake.NewSimpleClientset(), nil }},
			wantText: []string{"Helm Releases Found:** 0", "No Helm releases found in the cluster."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, rpcErr := callTool(t, tt.server, tt.tool, tt.args)
			if rpcErr != nil {
				t.Fatalf("handleToolsCall returned RPC error: %v", rpcErr)
			}
			if len(result.Content) != 1 {
				t.Fatalf("content length = %d, want 1", len(result.Content))
			}
			for _, want := range tt.wantText {
				if !strings.Contains(result.Content[0].Text, want) {
					t.Fatalf("result text %q missing %q", result.Content[0].Text, want)
				}
			}
		})
	}
}

func TestHandleToolsCallValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		tool string
		args map[string]interface{}
		want string
	}{
		{name: "get pod logs requires name", tool: "get_pod_logs", args: map[string]interface{}{"namespace": "apps"}, want: "Pod name is required"},
		{name: "can i requires verb and resource", tool: "can_i", args: map[string]interface{}{"verb": "", "resource": ""}, want: "verb and resource are required"},
		{name: "analyze subject permissions requires subject", tool: "analyze_subject_permissions", args: map[string]interface{}{}, want: "subject_kind and subject_name are required"},
		{name: "describe role requires name", tool: "describe_role", args: map[string]interface{}{}, want: "name is required"},
		{name: "find resource owners requires namespace", tool: "find_resource_owners", args: map[string]interface{}{}, want: "namespace is required"},
		{name: "analyze namespace requires namespace", tool: "analyze_namespace", args: map[string]interface{}{}, want: "namespace is required"},
		{name: "detect drift requires repo url", tool: "detect_drift", args: map[string]interface{}{}, want: "repo_url is required"},
		{name: "trigger openshift upgrade requires target version", tool: "trigger_openshift_upgrade", args: map[string]interface{}{}, want: "target_version is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, rpcErr := callTool(t, &Server{}, tt.tool, tt.args)
			if rpcErr != nil {
				t.Fatalf("handleToolsCall returned RPC error: %v", rpcErr)
			}
			if !result.IsError {
				t.Fatalf("expected tool %s to return error content", tt.tool)
			}
			if len(result.Content) != 1 {
				t.Fatalf("content length = %d, want 1", len(result.Content))
			}
			if !strings.Contains(result.Content[0].Text, tt.want) {
				t.Fatalf("result text %q missing %q", result.Content[0].Text, tt.want)
			}
		})
	}
}

func TestHandleToolsCallGetDeploymentsReturnsJSONList(t *testing.T) {
	result, rpcErr := callTool(t, &Server{clientFactory: func(clusterName string) (kubernetes.Interface, error) {
		return k8sfake.NewSimpleClientset(), nil
	}}, "get_deployments", map[string]interface{}{"namespace": "apps"})
	if rpcErr != nil {
		t.Fatalf("handleToolsCall returned RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected successful deployment listing, got error content: %q", result.Content[0].Text)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("failed to decode deployment JSON: %v", err)
	}
	items, ok := payload["items"].([]interface{})
	if !ok {
		t.Fatalf("expected items array in payload: %#v", payload)
	}
	if len(items) != 0 {
		t.Fatalf("expected empty deployment list, got %#v", items)
	}
}

func callTool(t *testing.T, s *Server, tool string, args map[string]interface{}) (CallToolResult, *Error) {
	t.Helper()

	if s.discoverer == nil {
		s.discoverer = stubDiscoverer{}
	}

	var buf bytes.Buffer
	s.writer = &buf

	params, err := json.Marshal(CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("failed to marshal params: %v", err)
	}

	s.handleToolsCall(context.Background(), &Request{ID: 1, Params: params})

	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *Error          `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error != nil {
		return CallToolResult{}, resp.Error
	}

	var result CallToolResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to decode tool result: %v", err)
	}
	return result, nil
}
