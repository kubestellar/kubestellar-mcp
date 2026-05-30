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

func TestToolFindPodIssues_NoPods(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	s := &Server{
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolFindPodIssues(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("toolFindPodIssues() returned error: %s", result)
	}

	if !strings.Contains(result, "No pod issues found") {
		t.Errorf("toolFindPodIssues() = %q, want 'No pod issues found'", result)
	}
}

func TestToolFindPodIssues_CrashLoopBackOff(t *testing.T) {
	client := k8sfake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "bad-pod", Namespace: "default"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "app",
					RestartCount: 10,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason:  "CrashLoopBackOff",
							Message: "container failed to start",
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

	wantStrings := []string{"bad-pod", "CrashLoopBackOff", "10 restarts"}
	for _, want := range wantStrings {
		if !strings.Contains(result, want) {
			t.Errorf("toolFindPodIssues() missing %q in:\n%s", want, result)
		}
	}
}

func TestToolFindPodIssues_ImagePullBackOff(t *testing.T) {
	client := k8sfake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "image-issue", Namespace: "apps"},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "web",
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason:  "ImagePullBackOff",
							Message: "Failed to pull image \"nonexistent:latest\": repository not found",
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

	wantStrings := []string{"image-issue", "ImagePullBackOff", "repository not found"}
	for _, want := range wantStrings {
		if !strings.Contains(result, want) {
			t.Errorf("toolFindPodIssues() missing %q in:\n%s", want, result)
		}
	}
}

func TestToolFindPodIssues_OOMKilled(t *testing.T) {
	client := k8sfake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "oom-pod", Namespace: "default"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "memory-hog",
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							Reason: "OOMKilled",
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

	wantStrings := []string{"oom-pod", "OOMKilled"}
	for _, want := range wantStrings {
		if !strings.Contains(result, want) {
			t.Errorf("toolFindPodIssues() missing %q in:\n%s", want, result)
		}
	}
}

func TestToolFindPodIssues_Unschedulable(t *testing.T) {
	client := k8sfake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pending-pod", Namespace: "default"},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			Conditions: []corev1.PodCondition{
				{
					Type:    corev1.PodScheduled,
					Status:  corev1.ConditionFalse,
					Message: "0/3 nodes are available: 3 Insufficient cpu",
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

	wantStrings := []string{"pending-pod", "Unschedulable", "Insufficient cpu"}
	for _, want := range wantStrings {
		if !strings.Contains(result, want) {
			t.Errorf("toolFindPodIssues() missing %q in:\n%s", want, result)
		}
	}
}

func TestToolFindPodIssues_IncludeCompleted(t *testing.T) {
	client := k8sfake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "succeeded-pod", Namespace: "default"},
			Status:     corev1.PodStatus{Phase: corev1.PodSucceeded},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "failed-pod", Namespace: "default"},
			Status:     corev1.PodStatus{Phase: corev1.PodFailed, Reason: "Error"},
		},
	)

	s := &Server{
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolFindPodIssues(context.Background(), map[string]interface{}{
		"include_completed": "true",
	})
	if isErr {
		t.Fatalf("toolFindPodIssues() returned error: %s", result)
	}

	if !strings.Contains(result, "failed-pod") {
		t.Errorf("toolFindPodIssues() should include failed pod: %s", result)
	}
}

func TestToolFindDeploymentIssues_NoIssues(t *testing.T) {
	client := k8sfake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "healthy-deploy", Namespace: "default"},
		Status: appsv1.DeploymentStatus{
			Replicas:      3,
			ReadyReplicas: 3,
		},
	})

	s := &Server{
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolFindDeploymentIssues(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("toolFindDeploymentIssues() returned error: %s", result)
	}

	if !strings.Contains(result, "No deployment issues found") {
		t.Errorf("toolFindDeploymentIssues() = %q, want 'No deployment issues found'", result)
	}
}

func TestToolFindDeploymentIssues_NotReady(t *testing.T) {
	client := k8sfake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "degraded-deploy", Namespace: "apps"},
		Status: appsv1.DeploymentStatus{
			Replicas:            5,
			ReadyReplicas:       3,
			UnavailableReplicas: 2,
		},
	})

	s := &Server{
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolFindDeploymentIssues(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("toolFindDeploymentIssues() returned error: %s", result)
	}

	wantStrings := []string{"degraded-deploy", "3/5 replicas ready", "2 replicas unavailable"}
	for _, want := range wantStrings {
		if !strings.Contains(result, want) {
			t.Errorf("toolFindDeploymentIssues() missing %q in:\n%s", want, result)
		}
	}
}

func TestToolFindDeploymentIssues_ProgressingFalse(t *testing.T) {
	client := k8sfake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "stuck-deploy", Namespace: "default"},
		Status: appsv1.DeploymentStatus{
			Replicas:      3,
			ReadyReplicas: 2,
			Conditions: []appsv1.DeploymentCondition{
				{
					Type:    appsv1.DeploymentProgressing,
					Status:  corev1.ConditionFalse,
					Message: "ReplicaSet has timed out progressing",
				},
			},
		},
	})

	s := &Server{
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolFindDeploymentIssues(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("toolFindDeploymentIssues() returned error: %s", result)
	}

	wantStrings := []string{"stuck-deploy", "Rollout stuck", "timed out progressing"}
	for _, want := range wantStrings {
		if !strings.Contains(result, want) {
			t.Errorf("toolFindDeploymentIssues() missing %q in:\n%s", want, result)
		}
	}
}

func TestToolCheckResourceLimits_NoIssues(t *testing.T) {
	client := k8sfake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "well-configured", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	})

	s := &Server{
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolCheckResourceLimits(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("toolCheckResourceLimits() returned error: %s", result)
	}

	if !strings.Contains(result, "All pods have resource limits configured") {
		t.Errorf("toolCheckResourceLimits() = %q, want 'All pods have resource limits configured'", result)
	}
}

func TestToolCheckResourceLimits_MissingLimits(t *testing.T) {
	client := k8sfake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "no-limits", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "app", Resources: corev1.ResourceRequirements{}},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	})

	s := &Server{
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolCheckResourceLimits(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("toolCheckResourceLimits() returned error: %s", result)
	}

	wantStrings := []string{"no-limits", "no CPU limit", "no memory limit", "no CPU request", "no memory request"}
	for _, want := range wantStrings {
		if !strings.Contains(result, want) {
			t.Errorf("toolCheckResourceLimits() missing %q in:\n%s", want, result)
		}
	}
}

func TestToolCheckSecurityIssues_NoIssues(t *testing.T) {
	readOnlyFS := true
	allowEscalation := false
	runAsUser := int64(1000)

	client := k8sfake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "secure-pod", Namespace: "apps"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app",
					SecurityContext: &corev1.SecurityContext{
						ReadOnlyRootFilesystem:   &readOnlyFS,
						AllowPrivilegeEscalation: &allowEscalation,
						RunAsUser:                &runAsUser,
					},
				},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	})

	s := &Server{
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolCheckSecurityIssues(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("toolCheckSecurityIssues() returned error: %s", result)
	}

	if !strings.Contains(result, "No obvious security issues found") {
		t.Errorf("toolCheckSecurityIssues() = %q, want 'No obvious security issues found'", result)
	}
}

func TestToolCheckSecurityIssues_Privileged(t *testing.T) {
	privileged := true
	client := k8sfake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "priv-pod", Namespace: "apps"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:            "priv-container",
					SecurityContext: &corev1.SecurityContext{Privileged: &privileged},
				},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	})

	s := &Server{
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolCheckSecurityIssues(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("toolCheckSecurityIssues() returned error: %s", result)
	}

	wantStrings := []string{"priv-pod", "is privileged"}
	for _, want := range wantStrings {
		if !strings.Contains(result, want) {
			t.Errorf("toolCheckSecurityIssues() missing %q in:\n%s", want, result)
		}
	}
}

func TestToolCheckSecurityIssues_HostNetwork(t *testing.T) {
	client := k8sfake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "host-net-pod", Namespace: "apps"},
		Spec: corev1.PodSpec{
			HostNetwork: true,
			Containers:  []corev1.Container{{Name: "app"}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	})

	s := &Server{
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolCheckSecurityIssues(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("toolCheckSecurityIssues() returned error: %s", result)
	}

	wantStrings := []string{"host-net-pod", "Uses host network"}
	for _, want := range wantStrings {
		if !strings.Contains(result, want) {
			t.Errorf("toolCheckSecurityIssues() missing %q in:\n%s", want, result)
		}
	}
}

func TestToolAnalyzeNamespace(t *testing.T) {
	client := k8sfake.NewSimpleClientset(
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "demo-ns",
				CreationTimestamp: metav1.Now(),
			},
			Status: corev1.NamespaceStatus{Phase: corev1.NamespaceActive},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "demo-ns"},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "deploy-1", Namespace: "demo-ns"},
			Status:     appsv1.DeploymentStatus{Replicas: 1, ReadyReplicas: 1},
		},
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "svc-1", Namespace: "demo-ns"},
		},
	)

	s := &Server{
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolAnalyzeNamespace(context.Background(), map[string]interface{}{
		"namespace": "demo-ns",
	})
	if isErr {
		t.Fatalf("toolAnalyzeNamespace() returned error: %s", result)
	}

	wantStrings := []string{
		"demo-ns",
		"Active",
		"Total: 1",
		"Running: 1",
		"Deployments: 1",
		"Services: 1",
	}
	for _, want := range wantStrings {
		if !strings.Contains(result, want) {
			t.Errorf("toolAnalyzeNamespace() missing %q in:\n%s", want, result)
		}
	}
}

func TestToolAnalyzeNamespace_MissingNamespace(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	s := &Server{
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolAnalyzeNamespace(context.Background(), map[string]interface{}{})
	if !isErr {
		t.Fatalf("toolAnalyzeNamespace() should return error when namespace is missing")
	}

	if !strings.Contains(result, "namespace is required") {
		t.Errorf("toolAnalyzeNamespace() error = %q, want 'namespace is required'", result)
	}
}

func TestToolGetWarningEvents_NoEvents(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	s := &Server{
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolGetWarningEvents(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("toolGetWarningEvents() returned error: %s", result)
	}

	if !strings.Contains(result, "No warning events found") {
		t.Errorf("toolGetWarningEvents() = %q, want 'No warning events found'", result)
	}
}

func TestToolGetWarningEvents_HasEvents(t *testing.T) {
	client := k8sfake.NewSimpleClientset(&corev1.Event{
		ObjectMeta: metav1.ObjectMeta{Name: "event-1", Namespace: "default"},
		Type:       "Warning",
		Reason:     "FailedScheduling",
		Message:    "0/3 nodes are available",
		Count:      5,
		InvolvedObject: corev1.ObjectReference{
			Kind: "Pod",
			Name: "pending-pod",
		},
		LastTimestamp: metav1.Now(),
	})

	s := &Server{
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolGetWarningEvents(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("toolGetWarningEvents() returned error: %s", result)
	}

	wantStrings := []string{"FailedScheduling", "0/3 nodes are available", "pending-pod", "occurred 5 times"}
	for _, want := range wantStrings {
		if !strings.Contains(result, want) {
			t.Errorf("toolGetWarningEvents() missing %q in:\n%s", want, result)
		}
	}
}

func TestToolFindPodIssues_ClientError(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	client.PrependReactor("list", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("API server unavailable")
	})

	s := &Server{
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolFindPodIssues(context.Background(), map[string]interface{}{})
	if !isErr {
		t.Fatal("toolFindPodIssues() expected error when client fails")
	}

	if !strings.Contains(result, "Failed to list pods") {
		t.Errorf("toolFindPodIssues() error = %q, want to contain 'Failed to list pods'", result)
	}
}
