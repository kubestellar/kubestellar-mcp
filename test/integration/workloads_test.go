//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/handlers"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/workloads"
)

// TestWorkloadsGetPods exercises pkg/mcp/server/workloads's get_pods MCP
// tool end-to-end against a real kube-apiserver (envtest): a Pod is created
// directly through a typed clientset, then the tool is invoked exactly as
// the real protocol server dispatches it — via workloads.Register into a
// handlers.Registry and handlers.Registry.Find("get_pods") — to confirm the
// handler's List call and output formatting work against a real API server
// rather than the fake.NewSimpleClientset the package's own unit tests use
// (get_pods_branches_test.go).
func TestWorkloadsGetPods(t *testing.T) {
	ctx := context.Background()

	clientset, err := kubernetes.NewForConfig(testCfg)
	if err != nil {
		t.Fatalf("kubernetes.NewForConfig: %v", err)
	}

	const namespace = "mcp-integration-workloads"
	const podName = "mcp-integration-pod"

	if _, err := clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create namespace: %v", err)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "app", Image: "registry.k8s.io/pause:3.9"},
			},
		},
	}
	if _, err := clientset.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create pod: %v", err)
	}

	reg := handlers.NewRegistry()
	workloads.Register(reg)

	handler := reg.Find("get_pods")
	if handler == nil {
		t.Fatal("get_pods tool not found in registry after workloads.Register")
	}

	deps := &handlers.Deps{
		ClientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return clientset, nil
		},
	}

	output, isError := handler(ctx, deps, map[string]interface{}{
		"namespace": namespace,
	})
	if isError {
		t.Fatalf("get_pods returned an error result: %s", output)
	}
	if !strings.Contains(output, podName) {
		t.Fatalf("get_pods output does not mention pod %q: %s", podName, output)
	}
	if !strings.Contains(output, "Found 1 pods") {
		t.Fatalf("get_pods output does not report 1 pod found: %s", output)
	}
}
