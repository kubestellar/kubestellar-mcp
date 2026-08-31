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

// The existing TestToolCheckSecurityIssues_* tests cover only three of the
// many branches in toolCheckSecurityIssues (diagnostics.go:293) — NoIssues,
// Privileged, and HostNetwork — leaving the function at 81.0% statement
// coverage. This file exercises the remaining branches so that a regression
// in any single "issue detector" arm (or in the skip / error / all-namespace
// wiring) is caught by a targeted test.

// TestToolCheckSecurityIssues_InvalidNamespaceArg covers the
// extractAndValidateNamespace error return (diagnostics.go:297-298).
func TestToolCheckSecurityIssues_InvalidNamespaceArg(t *testing.T) {
	s := &Server{
		clientFactory: func(string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(), nil
		},
	}

	result, isErr := s.toolCheckSecurityIssues(context.Background(), map[string]interface{}{
		"namespace": "Invalid_NS!",
	})
	if !isErr {
		t.Fatalf("expected error for invalid namespace, got: %s", result)
	}
	if !strings.HasPrefix(result, "error:") {
		t.Errorf("expected 'error:' prefix, got: %s", result)
	}
}

// TestToolCheckSecurityIssues_ClientFactoryError covers the
// getClientForCluster failure branch (diagnostics.go:302-303).
func TestToolCheckSecurityIssues_ClientFactoryError(t *testing.T) {
	s := &Server{
		clientFactory: func(string) (kubernetes.Interface, error) {
			return nil, errors.New("kubeconfig missing")
		},
	}

	result, isErr := s.toolCheckSecurityIssues(context.Background(), map[string]interface{}{})
	if !isErr {
		t.Fatalf("expected error when clientFactory fails, got: %s", result)
	}
	if !strings.Contains(result, "Failed to create client") {
		t.Errorf("expected 'Failed to create client' in result, got: %s", result)
	}
}

// TestToolCheckSecurityIssues_ListPodsError covers the pods.List error
// branch (diagnostics.go:313-314). The pods lister is monkey-patched via
// a fake reactor to force a non-nil error.
func TestToolCheckSecurityIssues_ListPodsError(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	client.PrependReactor("list", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("api-server unreachable")
	})

	s := &Server{
		clientFactory: func(string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolCheckSecurityIssues(context.Background(), map[string]interface{}{})
	if !isErr {
		t.Fatalf("expected error when list pods fails, got: %s", result)
	}
	if !strings.Contains(result, "Failed to list pods") {
		t.Errorf("expected 'Failed to list pods' in result, got: %s", result)
	}
}

// TestToolCheckSecurityIssues_NamespaceScoped covers the branch where a
// namespace argument is supplied — the alternate arm of the
// namespace=="" / namespace!="" ternary (diagnostics.go:307). It also
// serves as a smoke check that specifying a namespace does not filter out
// legitimately insecure pods in that namespace.
func TestToolCheckSecurityIssues_NamespaceScoped(t *testing.T) {
	client := k8sfake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "hpid-pod", Namespace: "apps"},
		Spec: corev1.PodSpec{
			HostPID:    true,
			Containers: []corev1.Container{{Name: "app"}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	})

	s := &Server{
		clientFactory: func(string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolCheckSecurityIssues(context.Background(), map[string]interface{}{
		"namespace": "apps",
	})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}
	if !strings.Contains(result, "Uses host PID namespace") {
		t.Errorf("expected HostPID finding in result, got: %s", result)
	}
}

// TestToolCheckSecurityIssues_SkipsKubeSystemAndTerminalPods covers the
// two "continue" branches at the top of the pod loop: the
// strings.HasPrefix(pod.Namespace, "kube-") skip and the
// PodSucceeded / PodFailed phase skip. Both pods should be completely
// absent from the output even though they carry real security issues.
func TestToolCheckSecurityIssues_SkipsKubeSystemAndTerminalPods(t *testing.T) {
	privileged := true
	client := k8sfake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "kube-system-priv", Namespace: "kube-system"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:            "priv",
					SecurityContext: &corev1.SecurityContext{Privileged: &privileged},
				}},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "succeeded-priv", Namespace: "apps"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:            "priv",
					SecurityContext: &corev1.SecurityContext{Privileged: &privileged},
				}},
			},
			Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "failed-priv", Namespace: "apps"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:            "priv",
					SecurityContext: &corev1.SecurityContext{Privileged: &privileged},
				}},
			},
			Status: corev1.PodStatus{Phase: corev1.PodFailed},
		},
	)

	s := &Server{
		clientFactory: func(string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolCheckSecurityIssues(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}
	if !strings.Contains(result, "No obvious security issues found") {
		t.Errorf("expected all pods to be skipped, got: %s", result)
	}
}

// TestToolCheckSecurityIssues_HostPIDAndHostIPC covers the HostPID and
// HostIPC pod-level branches (diagnostics.go:336-337, 339-340) — the two
// host-namespace flags that are not already exercised by the existing
// HostNetwork test.
func TestToolCheckSecurityIssues_HostPIDAndHostIPC(t *testing.T) {
	client := k8sfake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "hpid-pod", Namespace: "apps"},
			Spec: corev1.PodSpec{
				HostPID:    true,
				Containers: []corev1.Container{{Name: "app"}},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "hipc-pod", Namespace: "apps"},
			Spec: corev1.PodSpec{
				HostIPC:    true,
				Containers: []corev1.Container{{Name: "app"}},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		},
	)

	s := &Server{
		clientFactory: func(string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolCheckSecurityIssues(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}
	for _, want := range []string{"hpid-pod", "Uses host PID namespace", "hipc-pod", "Uses host IPC namespace"} {
		if !strings.Contains(result, want) {
			t.Errorf("missing %q in result:\n%s", want, result)
		}
	}
}

// TestToolCheckSecurityIssues_ContainerSecurityContextBranches covers the
// four container-level SecurityContext branches:
//
//   - sc.RunAsUser != nil && *sc.RunAsUser == 0  → runs-as-root
//   - sc.AllowPrivilegeEscalation truthy         → privilege escalation
//   - sc.ReadOnlyRootFilesystem falsy            → writable root fs
//   - sc == nil                                  → "no security context"
//
// Each branch is exercised by a dedicated container in a dedicated pod so
// the assertion can pinpoint which detector fired.
func TestToolCheckSecurityIssues_ContainerSecurityContextBranches(t *testing.T) {
	rootUser := int64(0)
	allowEscalation := true
	writableFS := false

	client := k8sfake.NewSimpleClientset(
		// runs-as-root only
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "root-user-pod", Namespace: "apps"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name: "root-app",
					SecurityContext: &corev1.SecurityContext{
						RunAsUser:                &rootUser,
						AllowPrivilegeEscalation: boolPtr(false),
						ReadOnlyRootFilesystem:   boolPtr(true),
					},
				}},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		},
		// AllowPrivilegeEscalation=true (explicit)
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "priv-esc-pod", Namespace: "apps"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name: "esc-app",
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &allowEscalation,
						ReadOnlyRootFilesystem:   boolPtr(true),
						RunAsUser:                int64Ptr(1000),
					},
				}},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		},
		// ReadOnlyRootFilesystem=false → writable root fs
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "writable-fs-pod", Namespace: "apps"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name: "wfs-app",
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: boolPtr(false),
						ReadOnlyRootFilesystem:   &writableFS,
						RunAsUser:                int64Ptr(1000),
					},
				}},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		},
		// sc == nil → "no security context"
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "no-sc-pod", Namespace: "apps"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "no-sc-app"}}, // SecurityContext left nil
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		},
	)

	s := &Server{
		clientFactory: func(string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolCheckSecurityIssues(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}

	wants := []string{
		"root-user-pod", "runs as root (UID 0)",
		"priv-esc-pod", "allows privilege escalation",
		"writable-fs-pod", "writable root filesystem",
		"no-sc-pod", "no security context",
	}
	for _, want := range wants {
		if !strings.Contains(result, want) {
			t.Errorf("missing %q in result:\n%s", want, result)
		}
	}
}

// TestToolCheckSecurityIssues_DockerSocketMount covers the sensitive-mount
// detection branch that fires when a container mounts /var/run/docker.sock.
func TestToolCheckSecurityIssues_DockerSocketMount(t *testing.T) {
	client := k8sfake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "docker-mount-pod", Namespace: "apps"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "dind-app",
				// Security context deliberately provided so the container
				// only trips the docker.sock branch, not the "no security
				// context" branch too.
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: boolPtr(false),
					ReadOnlyRootFilesystem:   boolPtr(true),
					RunAsUser:                int64Ptr(1000),
				},
				VolumeMounts: []corev1.VolumeMount{
					{Name: "docker-sock", MountPath: "/var/run/docker.sock"},
				},
			}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	})

	s := &Server{
		clientFactory: func(string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolCheckSecurityIssues(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}
	for _, want := range []string{"docker-mount-pod", "mounts Docker socket"} {
		if !strings.Contains(result, want) {
			t.Errorf("missing %q in result:\n%s", want, result)
		}
	}
}

// Small pointer helpers to keep the SecurityContext literals readable.
func boolPtr(b bool) *bool     { return &b }
func int64Ptr(i int64) *int64 { return &i }
