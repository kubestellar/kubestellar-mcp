package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// The existing diagnostics_test.go covers happy paths (CrashLoopBackOff,
// ImagePullBackOff, OOMKilled, Unschedulable, IncludeCompleted, NoPods) plus
// the pods.List() error branch. The tests below cover the remaining branches
// in toolFindPodIssues (diagnostics.go:16), lifting statement coverage from
// 85.2% -> 100%:
//
//   - extractAndValidateNamespace error (invalid RFC1123 argument)
//   - getClientForCluster error (nil client + no factory -> failure)
//   - namespace-scoped List (non-empty "namespace" arg)
//   - Skip of PodSucceeded/PodFailed when include_completed is not "true"
//   - Long-message truncation ( >100 char Waiting.Message )
//   - "Container running but not ready" branch
//   - Init container waiting branch

// TestToolFindPodIssues_InvalidNamespace covers the
// extractAndValidateNamespace error path at diagnostics.go:19-21.
func TestToolFindPodIssues_InvalidNamespace(t *testing.T) {
	s := &Server{
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(), nil
		},
	}

	result, isErr := s.toolFindPodIssues(context.Background(), map[string]interface{}{
		"namespace": "INVALID_UPPERCASE_NS",
	})
	if !isErr {
		t.Fatalf("toolFindPodIssues() expected error for invalid namespace, got: %s", result)
	}
	if !strings.HasPrefix(result, "error:") {
		t.Errorf("toolFindPodIssues() error = %q, want prefix 'error:'", result)
	}
}

// TestToolFindPodIssues_ClientFactoryError covers the getClientForCluster
// error path at diagnostics.go:25-27 (distinct from the pods.List error path
// already covered by TestToolFindPodIssues_ClientError).
func TestToolFindPodIssues_ClientFactoryError(t *testing.T) {
	s := &Server{
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return nil, errors.New("kubeconfig missing")
		},
	}

	result, isErr := s.toolFindPodIssues(context.Background(), map[string]interface{}{})
	if !isErr {
		t.Fatalf("toolFindPodIssues() expected error for client factory failure, got: %s", result)
	}
	if !strings.Contains(result, "Failed to create client") {
		t.Errorf("toolFindPodIssues() error = %q, want to contain 'Failed to create client'", result)
	}
	if !strings.Contains(result, "kubeconfig missing") {
		t.Errorf("toolFindPodIssues() error = %q, want to contain wrapped cause 'kubeconfig missing'", result)
	}
}

// TestToolFindPodIssues_NamespaceScopedList covers the namespace != ""
// branch at diagnostics.go:33-34. We verify the branch is taken by
// installing a reactor that asserts the list action's namespace matches.
func TestToolFindPodIssues_NamespaceScopedList(t *testing.T) {
	inScope := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "in-ns", Namespace: "app-a"},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
		},
	}
	outOfScope := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "other-ns", Namespace: "app-b"},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
		},
	}
	client := k8sfake.NewSimpleClientset(inScope, outOfScope)

	var listedNs string
	client.PrependReactor("list", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		listedNs = action.GetNamespace()
		return false, nil, nil // fall through to default tracker
	})

	s := &Server{
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolFindPodIssues(context.Background(), map[string]interface{}{
		"namespace": "app-a",
	})
	if isErr {
		t.Fatalf("toolFindPodIssues() returned error: %s", result)
	}

	if listedNs != "app-a" {
		t.Errorf("List reactor observed namespace %q, want %q — namespace-scoped List branch not taken", listedNs, "app-a")
	}
	// Only the app-a pod should appear in the report.
	if !strings.Contains(result, "in-ns") {
		t.Errorf("toolFindPodIssues() missing 'in-ns' in:\n%s", result)
	}
	if strings.Contains(result, "other-ns") {
		t.Errorf("toolFindPodIssues() unexpectedly reported out-of-scope pod 'other-ns':\n%s", result)
	}
}

// TestToolFindPodIssues_SkipsCompletedByDefault covers the
// "!includeCompleted && (PodSucceeded||PodFailed)" continue branch at
// diagnostics.go:46-48 when include_completed is *not* passed.
func TestToolFindPodIssues_SkipsCompletedByDefault(t *testing.T) {
	completed := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "batch-done", Namespace: "default"},
		Status: corev1.PodStatus{
			Phase: corev1.PodSucceeded,
		},
	}
	alsoDone := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "batch-failed", Namespace: "default"},
		Status: corev1.PodStatus{
			Phase:  corev1.PodFailed,
			Reason: "Evicted",
		},
	}
	client := k8sfake.NewSimpleClientset(completed, alsoDone)

	s := &Server{
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	// No include_completed arg — both terminal pods must be skipped and
	// the function must report the no-issues path.
	result, isErr := s.toolFindPodIssues(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("toolFindPodIssues() returned error: %s", result)
	}
	if !strings.Contains(result, "No pod issues found") {
		t.Errorf("toolFindPodIssues() = %q, want 'No pod issues found' (terminal pods must be skipped)", result)
	}
	if strings.Contains(result, "batch-done") || strings.Contains(result, "batch-failed") {
		t.Errorf("toolFindPodIssues() unexpectedly included a completed pod:\n%s", result)
	}
}

// TestToolFindPodIssues_LongWaitingMessageTruncated covers the
// `if len(msg) > 100 { msg = msg[:100] + "..." }` branch at
// diagnostics.go:53-55.
func TestToolFindPodIssues_LongWaitingMessageTruncated(t *testing.T) {
	longMsg := strings.Repeat("A", 250)
	client := k8sfake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "wordy-pod", Namespace: "default"},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "app",
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason:  "ImagePullBackOff",
							Message: longMsg,
						},
					},
				},
			},
		},
	})

	s := &Server{
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolFindPodIssues(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("toolFindPodIssues() returned error: %s", result)
	}
	if !strings.Contains(result, "...") {
		t.Errorf("toolFindPodIssues() did not truncate long Waiting.Message (expected '...' suffix):\n%s", result)
	}
	// Full 250-char string should NOT appear.
	if strings.Contains(result, longMsg) {
		t.Errorf("toolFindPodIssues() included the full un-truncated 250-char message:\n%s", result)
	}
}

// TestToolFindPodIssues_ContainerRunningNotReady covers the
// `!cs.Ready && cs.State.Running != nil` branch at diagnostics.go:68-70.
func TestToolFindPodIssues_ContainerRunningNotReady(t *testing.T) {
	client := k8sfake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "sluggish-pod", Namespace: "default"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:  "app",
					Ready: false,
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{},
					},
				},
			},
		},
	})

	s := &Server{
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolFindPodIssues(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("toolFindPodIssues() returned error: %s", result)
	}
	if !strings.Contains(result, "running but not ready") {
		t.Errorf("toolFindPodIssues() missing 'running but not ready' branch text in:\n%s", result)
	}
	if !strings.Contains(result, "sluggish-pod") {
		t.Errorf("toolFindPodIssues() missing pod name 'sluggish-pod' in:\n%s", result)
	}
}

// TestToolFindPodIssues_InitContainerWaiting covers the init-container
// waiting branch at diagnostics.go:73-77 (the second `for _, cs := range
// pod.Status.InitContainerStatuses` loop).
func TestToolFindPodIssues_InitContainerWaiting(t *testing.T) {
	client := k8sfake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "stuck-init", Namespace: "default"},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			InitContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "wait-for-db",
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason: "PodInitializing",
						},
					},
				},
			},
		},
	})

	s := &Server{
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolFindPodIssues(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("toolFindPodIssues() returned error: %s", result)
	}
	if !strings.Contains(result, "Init container wait-for-db waiting: PodInitializing") {
		t.Errorf("toolFindPodIssues() missing init container waiting branch text in:\n%s", result)
	}
}
