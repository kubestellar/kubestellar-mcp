package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// The existing suite for toolCheckResourceLimits in diagnostics_test.go covers
// the happy-path pod-with-limits and the missing-limits report, leaving the
// following branches at diagnostics.go:220–288 uncovered:
//
//   - line 224: extractAndValidateNamespace returns an error → "error: ..."
//   - line 229: getClientForCluster returns an error → "Failed to create client"
//   - line 236: namespace explicitly provided (non-empty) → Pods(namespace) arm
//   - line 240: Pods().List returns an error → "Failed to list pods"
//   - line 249: pod.Status.Phase == PodSucceeded/PodFailed → skip continue arm
//
// These tests pin each remaining branch so a regression that dropped the error
// wrapper, swapped the namespaced/empty selector, or removed the terminal-phase
// filter would be caught.

// TestToolCheckResourceLimits_InvalidNamespaceArg covers the
// extractAndValidateNamespace error branch (line 224).
func TestToolCheckResourceLimits_InvalidNamespaceArg(t *testing.T) {
	s := &Server{
		clientFactory: func(string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(), nil
		},
	}
	result, isErr := s.toolCheckResourceLimits(context.Background(), map[string]interface{}{
		"namespace": "Invalid_NS!",
	})
	if !isErr {
		t.Fatalf("expected error for invalid namespace, got: %s", result)
	}
	if !strings.HasPrefix(result, "error:") {
		t.Errorf("expected 'error:' prefix, got: %s", result)
	}
}

// TestToolCheckResourceLimits_ClientFactoryError covers the getClientForCluster
// failure branch (line 229).
func TestToolCheckResourceLimits_ClientFactoryError(t *testing.T) {
	s := &Server{
		clientFactory: func(string) (kubernetes.Interface, error) {
			return nil, errors.New("kubeconfig missing")
		},
	}
	result, isErr := s.toolCheckResourceLimits(context.Background(), map[string]interface{}{})
	if !isErr {
		t.Fatalf("expected error when clientFactory fails, got: %s", result)
	}
	if !strings.Contains(result, "Failed to create client") {
		t.Errorf("expected 'Failed to create client' in result, got: %s", result)
	}
}

// TestToolCheckResourceLimits_NamespacedListError covers both the
// namespace-explicitly-set branch (line 236) and the Pods().List() error
// branch (line 240) using a fake reactor to reject the request.
func TestToolCheckResourceLimits_NamespacedListError(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	client.PrependReactor("list", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		listAction, ok := action.(k8stesting.ListAction)
		if !ok {
			t.Fatalf("expected ListAction, got %T", action)
		}
		if got, want := listAction.GetNamespace(), "target-ns"; got != want {
			t.Errorf("list pods called on namespace %q, want %q", got, want)
		}
		return true, nil, errors.New("forbidden: user cannot list pods")
	})
	s := &Server{
		clientFactory: func(string) (kubernetes.Interface, error) { return client, nil },
	}
	result, isErr := s.toolCheckResourceLimits(context.Background(), map[string]interface{}{
		"namespace": "target-ns",
	})
	if !isErr {
		t.Fatalf("expected error when list pods fails, got: %s", result)
	}
	if !strings.Contains(result, "Failed to list pods") {
		t.Errorf("expected 'Failed to list pods' in result, got: %s", result)
	}
}

// TestToolCheckResourceLimits_TerminalPodsSkipped pins the PodSucceeded /
// PodFailed skip branch (line 249). A pod in a terminal phase with no
// resource limits must NOT appear in the report — only the running pod
// without limits should be flagged.
func TestToolCheckResourceLimits_TerminalPodsSkipped(t *testing.T) {
	succeededPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "job-done", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				// No limits, but the pod is in a terminal phase.
				{Name: "worker", Resources: corev1.ResourceRequirements{}},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
	}
	failedPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "job-crashed", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "worker", Resources: corev1.ResourceRequirements{}},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodFailed},
	}
	runningPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "app", Resources: corev1.ResourceRequirements{}},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	client := k8sfake.NewSimpleClientset(succeededPod, failedPod, runningPod)
	s := &Server{
		clientFactory: func(string) (kubernetes.Interface, error) { return client, nil },
	}
	result, isErr := s.toolCheckResourceLimits(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}
	if !strings.Contains(result, "Found 1 pods without proper resource limits") {
		t.Errorf("expected exactly 1 pod flagged, got:\n%s", result)
	}
	if !strings.Contains(result, "web") {
		t.Errorf("expected running pod 'web' to be flagged, got:\n%s", result)
	}
	for _, terminalName := range []string{"job-done", "job-crashed"} {
		if strings.Contains(result, terminalName) {
			t.Errorf("terminal-phase pod %q must NOT appear in report, got:\n%s", terminalName, result)
		}
	}
}

// TestToolCheckResourceLimits_PartialLimits covers the mixed-issue branches
// where some (but not all) of CPU limit / memory limit / CPU request /
// memory request are set. This exercises the four independent
// `container.Resources.{Limits,Requests}.{Cpu,Memory}().IsZero()` arms.
func TestToolCheckResourceLimits_PartialLimits(t *testing.T) {
	// Only CPU limit + memory request set → the memory-limit and
	// CPU-request arms both fire, but the other two do not.
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "partial", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "app",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("500m"),
					},
					Requests: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("64Mi"),
					},
				},
			}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	client := k8sfake.NewSimpleClientset(pod)
	s := &Server{
		clientFactory: func(string) (kubernetes.Interface, error) { return client, nil },
	}
	result, isErr := s.toolCheckResourceLimits(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}
	// The two zero arms are reported.
	for _, want := range []string{"no memory limit", "no CPU request"} {
		if !strings.Contains(result, want) {
			t.Errorf("expected %q in output, got:\n%s", want, result)
		}
	}
	// The two set arms are NOT reported.
	for _, bad := range []string{"no CPU limit", "no memory request"} {
		if strings.Contains(result, bad) {
			t.Errorf("did NOT expect %q in output, got:\n%s", bad, result)
		}
	}
}

// TestToolCheckResourceLimits_ClusterArgPropagated ensures the "cluster" arg
// is threaded through clientFactory (previously implicit only).
func TestToolCheckResourceLimits_ClusterArgPropagated(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	var got string
	s := &Server{
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			got = clusterName
			return client, nil
		},
	}
	_, isErr := s.toolCheckResourceLimits(context.Background(), map[string]interface{}{
		"cluster": "edge-42",
	})
	if isErr {
		t.Fatalf("unexpected error path")
	}
	if got != "edge-42" {
		t.Errorf("clientFactory received cluster=%q, want %q", got, "edge-42")
	}
}
