package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// TestToolAnalyzeNamespace_InvalidNamespaceArg covers the
// extractAndValidateNamespace error branch (not the "missing" branch, which is
// tested elsewhere).
func TestToolAnalyzeNamespace_InvalidNamespaceArg(t *testing.T) {
	s := &Server{
		clientFactory: func(string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(), nil
		},
	}

	// A namespace containing an invalid RFC 1123 character forces
	// extractAndValidateNamespace to return an error.
	result, isErr := s.toolAnalyzeNamespace(context.Background(), map[string]interface{}{
		"namespace": "Invalid_NS!",
	})
	if !isErr {
		t.Fatalf("expected error for invalid namespace, got: %s", result)
	}
	if !strings.HasPrefix(result, "error:") {
		t.Errorf("expected 'error:' prefix, got: %s", result)
	}
}

// TestToolAnalyzeNamespace_ClientFactoryError covers the
// getClientForCluster failure branch.
func TestToolAnalyzeNamespace_ClientFactoryError(t *testing.T) {
	s := &Server{
		clientFactory: func(string) (kubernetes.Interface, error) {
			return nil, errors.New("kubeconfig missing")
		},
	}

	result, isErr := s.toolAnalyzeNamespace(context.Background(), map[string]interface{}{
		"namespace": "demo-ns",
	})
	if !isErr {
		t.Fatalf("expected error when clientFactory fails, got: %s", result)
	}
	if !strings.Contains(result, "Failed to create client") {
		t.Errorf("expected 'Failed to create client' in result, got: %s", result)
	}
}

// TestToolAnalyzeNamespace_NamespaceGetError covers the branch where
// Namespaces().Get(...) fails (e.g. namespace does not exist).
func TestToolAnalyzeNamespace_NamespaceGetError(t *testing.T) {
	client := k8sfake.NewSimpleClientset() // no namespace object
	s := &Server{
		clientFactory: func(string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolAnalyzeNamespace(context.Background(), map[string]interface{}{
		"namespace": "does-not-exist",
	})
	if !isErr {
		t.Fatalf("expected error when namespace is not found, got: %s", result)
	}
	if !strings.Contains(result, "Failed to get namespace") {
		t.Errorf("expected 'Failed to get namespace' in result, got: %s", result)
	}
}

// TestToolAnalyzeNamespace_FullDetails covers the populated-list branches:
// resource quotas, limit ranges, pending/failed/crashing pods, unhealthy
// deployments, pending PVCs, and warning events.
func TestToolAnalyzeNamespace_FullDetails(t *testing.T) {
	nsName := "prod-ns"

	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "compute-quota", Namespace: nsName},
		Status: corev1.ResourceQuotaStatus{
			Hard: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("4"),
			},
			Used: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("2"),
			},
		},
	}
	limitRange := &corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{Name: "default-limits", Namespace: nsName},
	}

	// Pod exercising the CrashLoopBackOff branch (Waiting.Reason).
	crashingPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "crash-pod", Namespace: nsName},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					RestartCount: 1,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
					},
				},
			},
		},
	}
	// Pod exercising the RestartCount > 5 branch.
	restartsPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "restart-pod", Namespace: nsName},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{RestartCount: 10, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
			},
		},
	}
	// Healthy running pod (control).
	healthyPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "ok-pod", Namespace: nsName},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{RestartCount: 0, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
			},
		},
	}
	pendingPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pending-pod", Namespace: nsName},
		Status:     corev1.PodStatus{Phase: corev1.PodPending},
	}
	failedPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "failed-pod", Namespace: nsName},
		Status:     corev1.PodStatus{Phase: corev1.PodFailed},
	}

	unhealthyDeploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "flaky", Namespace: nsName},
		Status:     appsv1.DeploymentStatus{Replicas: 3, ReadyReplicas: 1},
	}
	healthyDeploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "steady", Namespace: nsName},
		Status:     appsv1.DeploymentStatus{Replicas: 2, ReadyReplicas: 2},
	}

	pendingPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: nsName},
		Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending},
	}

	warningEvent := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{Name: "warn-1", Namespace: nsName},
		Type:       "Warning",
		Reason:     "FailedScheduling",
		Message:    "no nodes available",
		InvolvedObject: corev1.ObjectReference{
			Kind: "Pod", Name: "pending-pod", Namespace: nsName,
		},
	}

	client := k8sfake.NewSimpleClientset(
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: nsName, CreationTimestamp: metav1.Now()},
			Status:     corev1.NamespaceStatus{Phase: corev1.NamespaceActive},
		},
		quota,
		limitRange,
		crashingPod, restartsPod, healthyPod, pendingPod, failedPod,
		unhealthyDeploy, healthyDeploy,
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: nsName}},
		pendingPVC,
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm-1", Namespace: nsName}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "sec-1", Namespace: nsName}},
		warningEvent,
	)

	s := &Server{
		clientFactory: func(string) (kubernetes.Interface, error) { return client, nil },
	}

	result, isErr := s.toolAnalyzeNamespace(context.Background(), map[string]interface{}{
		"namespace": nsName,
	})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}

	// Each substring pins a distinct branch or output line.
	wantSubs := []string{
		"Namespace Analysis: " + nsName,
		"Resource Quotas",
		"compute-quota",
		"Limit Ranges",
		"default-limits",
		"Total: 5",
		"Running: 3",
		"Pending: 1",
		"Failed: 1",
		"Crashing/Restarting: 2",
		"Deployments: 2",
		"1 unhealthy",
		"Services: 1",
		"PVCs: 1",
		"1 pending",
		"ConfigMaps: 1",
		"Secrets: 1",
		"Recent Warnings: 1 events",
	}
	for _, want := range wantSubs {
		if !strings.Contains(result, want) {
			t.Errorf("missing %q in analysis output:\n%s", want, result)
		}
	}
}

// TestToolAnalyzeNamespace_MinimalPathsSuppressed verifies the negative
// branches: with no warnings/pending/failed/crashing/unhealthy resources, the
// corresponding decorated lines are omitted.
func TestToolAnalyzeNamespace_MinimalPathsSuppressed(t *testing.T) {
	client := k8sfake.NewSimpleClientset(
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "quiet", CreationTimestamp: metav1.Now()},
			Status:     corev1.NamespaceStatus{Phase: corev1.NamespaceActive},
		},
	)
	s := &Server{
		clientFactory: func(string) (kubernetes.Interface, error) { return client, nil },
	}
	result, isErr := s.toolAnalyzeNamespace(context.Background(), map[string]interface{}{
		"namespace": "quiet",
	})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}
	// No Resource Quotas / Limit Ranges sections should be emitted.
	forbidden := []string{"Resource Quotas", "Limit Ranges", "Pending:", "Failed:", "Crashing/Restarting:", "unhealthy", "pending ⚠️", "Recent Warnings"}
	for _, bad := range forbidden {
		if strings.Contains(result, bad) {
			t.Errorf("did not expect %q in minimal-analysis output:\n%s", bad, result)
		}
	}
	// Baseline lines still present.
	for _, want := range []string{"Namespace Analysis: quiet", "Total: 0", "Deployments: 0", "Services: 0"} {
		if !strings.Contains(result, want) {
			t.Errorf("expected %q in output:\n%s", want, result)
		}
	}
}

// TestToolAnalyzeNamespace_ClusterArgPropagated ensures the "cluster" arg is
// threaded through clientFactory (previously implicit only).
func TestToolAnalyzeNamespace_ClusterArgPropagated(t *testing.T) {
	client := k8sfake.NewSimpleClientset(
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "ns", CreationTimestamp: metav1.Now()},
			Status:     corev1.NamespaceStatus{Phase: corev1.NamespaceActive},
		},
	)
	var got string
	s := &Server{
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			got = clusterName
			return client, nil
		},
	}
	_, isErr := s.toolAnalyzeNamespace(context.Background(), map[string]interface{}{
		"cluster":   "edge-1",
		"namespace": "ns",
	})
	if isErr {
		t.Fatalf("unexpected error path")
	}
	if got != "edge-1" {
		t.Errorf("clientFactory received cluster=%q, want %q", got, "edge-1")
	}
}

// Compile-time guard: keep the k8stesting import used in case future tests
// need to inject a reactor. (Silences unused-import in the file above.)
var _ = k8stesting.Fake{}
var _ = runtime.Object(nil)
