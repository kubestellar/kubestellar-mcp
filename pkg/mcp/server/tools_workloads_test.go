package server

import (
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestToolGetPodsSuccess(t *testing.T) {
	now := metav1.NewTime(time.Date(2024, time.June, 1, 12, 0, 0, 0, time.UTC))
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "web-abc123", Namespace: "production"},
					Status: corev1.PodStatus{
						Phase:     corev1.PodRunning,
						StartTime: &now,
						ContainerStatuses: []corev1.ContainerStatus{
							{Name: "nginx", Ready: true},
							{Name: "sidecar", Ready: true},
						},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "worker-xyz789", Namespace: "production"},
					Status: corev1.PodStatus{
						Phase:     corev1.PodPending,
						StartTime: nil,
						ContainerStatuses: []corev1.ContainerStatus{
							{Name: "worker", Ready: false},
						},
					},
				},
			), nil
		},
	}

	result, rpcErr := callTool(t, server, "get_pods", map[string]interface{}{"namespace": "production"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}

	text := result.Content[0].Text
	if !strings.Contains(text, "Found 2 pods") {
		t.Fatalf("expected 'Found 2 pods', got: %s", text)
	}
	if !strings.Contains(text, "web-abc123") {
		t.Fatalf("expected 'web-abc123' in output, got: %s", text)
	}
	if !strings.Contains(text, "worker-xyz789") {
		t.Fatalf("expected 'worker-xyz789' in output, got: %s", text)
	}
	if !strings.Contains(text, "2/2") {
		t.Fatalf("expected '2/2' ready containers for web pod, got: %s", text)
	}
	if !strings.Contains(text, "0/1") {
		t.Fatalf("expected '0/1' ready containers for worker pod, got: %s", text)
	}
}

func TestToolGetPodsWithLabelSelector(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "labeled-pod",
						Namespace: "default",
						Labels:    map[string]string{"app": "web"},
					},
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
				},
			), nil
		},
	}

	result, rpcErr := callTool(t, server, "get_pods", map[string]interface{}{
		"namespace":      "default",
		"label_selector": "app=web",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}
	// The fake client doesn't actually filter by label, but we verify the tool
	// accepts and passes the label_selector parameter without error
	if !strings.Contains(result.Content[0].Text, "labeled-pod") {
		t.Fatalf("expected 'labeled-pod' in output, got: %s", result.Content[0].Text)
	}
}

func TestToolGetServicesSuccess(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{Name: "api-gateway", Namespace: "ingress"},
					Spec: corev1.ServiceSpec{
						Type:      corev1.ServiceTypeLoadBalancer,
						ClusterIP: "10.96.0.100",
						Ports: []corev1.ServicePort{
							{Port: 80, Protocol: corev1.ProtocolTCP},
							{Port: 443, NodePort: 30443, Protocol: corev1.ProtocolTCP},
						},
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{Name: "backend", Namespace: "ingress"},
					Spec: corev1.ServiceSpec{
						Type:      corev1.ServiceTypeClusterIP,
						ClusterIP: "10.96.0.200",
						Ports: []corev1.ServicePort{
							{Port: 8080, Protocol: corev1.ProtocolTCP},
						},
					},
				},
			), nil
		},
	}

	result, rpcErr := callTool(t, server, "get_services", map[string]interface{}{"namespace": "ingress"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}

	text := result.Content[0].Text
	if !strings.Contains(text, "Found 2 services") {
		t.Fatalf("expected 'Found 2 services', got: %s", text)
	}
	if !strings.Contains(text, "api-gateway") {
		t.Fatalf("expected 'api-gateway' in output, got: %s", text)
	}
	if !strings.Contains(text, "LoadBalancer") {
		t.Fatalf("expected 'LoadBalancer' type in output, got: %s", text)
	}
	if !strings.Contains(text, "10.96.0.100") {
		t.Fatalf("expected ClusterIP in output, got: %s", text)
	}
	if !strings.Contains(text, "443:30443/TCP") {
		t.Fatalf("expected '443:30443/TCP' port format, got: %s", text)
	}
}

func TestToolGetNodesSuccess(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "worker-1",
						Labels: map[string]string{"node-role.kubernetes.io/worker": ""},
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
						NodeInfo: corev1.NodeSystemInfo{KubeletVersion: "v1.30.2"},
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "control-plane-1",
						Labels: map[string]string{"node-role.kubernetes.io/control-plane": ""},
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
						},
						NodeInfo: corev1.NodeSystemInfo{KubeletVersion: "v1.30.2"},
					},
				},
			), nil
		},
	}

	result, rpcErr := callTool(t, server, "get_nodes", map[string]interface{}{})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}

	text := result.Content[0].Text
	if !strings.Contains(text, "Found 2 nodes") {
		t.Fatalf("expected 'Found 2 nodes', got: %s", text)
	}
	if !strings.Contains(text, "worker-1") {
		t.Fatalf("expected 'worker-1' in output, got: %s", text)
	}
	if !strings.Contains(text, "Ready") {
		t.Fatalf("expected 'Ready' status for worker-1, got: %s", text)
	}
	if !strings.Contains(text, "NotReady") {
		t.Fatalf("expected 'NotReady' status for control-plane-1, got: %s", text)
	}
	if !strings.Contains(text, "v1.30.2") {
		t.Fatalf("expected kubelet version in output, got: %s", text)
	}
	if !strings.Contains(text, "worker") {
		t.Fatalf("expected 'worker' role in output, got: %s", text)
	}
}

func TestToolGetEventsSuccess(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(
				&corev1.Event{
					ObjectMeta:     metav1.ObjectMeta{Name: "evt-1", Namespace: "apps"},
					Type:           "Warning",
					Message:        "Back-off restarting failed container",
					InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "failing-pod"},
				},
				&corev1.Event{
					ObjectMeta:     metav1.ObjectMeta{Name: "evt-2", Namespace: "apps"},
					Type:           "Normal",
					Message:        "Successfully pulled image",
					InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "web-pod"},
				},
			), nil
		},
	}

	result, rpcErr := callTool(t, server, "get_events", map[string]interface{}{"namespace": "apps"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}

	text := result.Content[0].Text
	if !strings.Contains(text, "Found 2 events") {
		t.Fatalf("expected 'Found 2 events', got: %s", text)
	}
	if !strings.Contains(text, "[Warning]") {
		t.Fatalf("expected '[Warning]' event type, got: %s", text)
	}
	if !strings.Contains(text, "[Normal]") {
		t.Fatalf("expected '[Normal]' event type, got: %s", text)
	}
	if !strings.Contains(text, "Back-off restarting") {
		t.Fatalf("expected event message in output, got: %s", text)
	}
	if !strings.Contains(text, "Pod/failing-pod") {
		t.Fatalf("expected 'Pod/failing-pod' involved object, got: %s", text)
	}
}

func TestToolDescribePodSuccess(t *testing.T) {
	now := metav1.NewTime(time.Date(2024, time.June, 1, 12, 0, 0, 0, time.UTC))
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "my-pod", Namespace: "default"},
					Spec: corev1.PodSpec{
						NodeName:   "worker-1",
						Containers: []corev1.Container{{Name: "app", Image: "myapp:v2.1"}},
					},
					Status: corev1.PodStatus{
						Phase:     corev1.PodRunning,
						PodIP:     "10.244.1.5",
						StartTime: &now,
						ContainerStatuses: []corev1.ContainerStatus{
							{Name: "app", Ready: true, RestartCount: 3},
						},
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
			), nil
		},
	}

	result, rpcErr := callTool(t, server, "describe_pod", map[string]interface{}{
		"name":      "my-pod",
		"namespace": "default",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}

	text := result.Content[0].Text
	for _, want := range []string{
		"Name: my-pod",
		"Namespace: default",
		"Status: Running",
		"Node: worker-1",
		"IP: 10.244.1.5",
		"app (image: myapp:v2.1)",
		"app: ready, restarts: 3",
		"Ready: True",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in output, got: %s", want, text)
		}
	}
}

func TestToolDescribePodMissingName(t *testing.T) {
	server := &Server{discoverer: stubDiscoverer{}}
	result, rpcErr := callTool(t, server, "describe_pod", map[string]interface{}{"namespace": "default"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatal("expected error for missing pod name")
	}
	if !strings.Contains(result.Content[0].Text, "Pod name is required") {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}
}

func TestToolGetPodLogsValidation(t *testing.T) {
	server := &Server{discoverer: stubDiscoverer{}}
	result, rpcErr := callTool(t, server, "get_pod_logs", map[string]interface{}{"namespace": "default"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatal("expected error for missing pod name")
	}
	if !strings.Contains(result.Content[0].Text, "Pod name is required") {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}
}
