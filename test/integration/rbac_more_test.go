//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/handlers"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/rbac"
)

// TestRBACAuditKubeconfig exercises pkg/mcp/server/rbac's audit_kubeconfig
// MCP tool end-to-end against a real kube-apiserver (envtest). Unlike
// can_i/analyze_subject_permissions (see TestRBACRolesAndBindings), this
// tool never calls SelfSubjectAccessReview, so it is not affected by
// envtest's default RBAC-authorizer bypass; it only needs a real kubeconfig
// file on disk, which writeIntegrationKubeconfig (cluster_test.go) already
// produces for the shared envtest control plane.
func TestRBACAuditKubeconfig(t *testing.T) {
	ctx := context.Background()

	kubeconfigPath := writeIntegrationKubeconfig(t)

	reg := handlers.NewRegistry()
	rbac.Register(reg)

	deps := &handlers.Deps{
		Kubeconfig: kubeconfigPath,
	}

	handler := reg.Find("audit_kubeconfig")
	if handler == nil {
		t.Fatal("audit_kubeconfig tool not found in registry after rbac.Register")
	}

	output, isError := handler(ctx, deps, map[string]interface{}{})
	if isError {
		t.Fatalf("audit_kubeconfig returned an error result: %s", output)
	}
	if !strings.Contains(output, integrationClusterName) {
		t.Fatalf("audit_kubeconfig output does not mention context %q: %s", integrationClusterName, output)
	}
	if !strings.Contains(output, "Accessible") {
		t.Fatalf("audit_kubeconfig output does not report accessibility: %s", output)
	}
}

// TestRBACFindResourceOwners exercises pkg/mcp/server/rbac's
// find_resource_owners MCP tool end-to-end against a real kube-apiserver
// (envtest): a Deployment and a directly-owned Pod are created via the
// typed clientset (envtest runs no controller-manager, so the ReplicaSet
// link is not materialized — the Pod's OwnerReference is set by hand, the
// same way diagnostics_test.go seeds ownership), then the tool is driven
// exactly as the real protocol server dispatches it and asserted to report
// the owner reference on the live object.
func TestRBACFindResourceOwners(t *testing.T) {
	ctx := context.Background()

	clientset, err := kubernetes.NewForConfig(testCfg)
	if err != nil {
		t.Fatalf("kubernetes.NewForConfig: %v", err)
	}

	const namespace = "mcp-integration-rbac-owners"
	const deploymentName = "mcp-integration-owner-deployment"
	const podName = "mcp-integration-owned-pod"

	if _, err := clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create namespace: %v", err)
	}

	replicas := int32(1)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: deploymentName, Namespace: namespace},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": deploymentName}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": deploymentName}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app", Image: "busybox"}},
				},
			},
		},
	}
	createdDeployment, err := clientset.AppsV1().Deployments(namespace).Create(ctx, deployment, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create deployment: %v", err)
	}

	controller := true
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Labels:    map[string]string{"managed-by": "mcp-integration"},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       createdDeployment.Name,
				UID:        createdDeployment.UID,
				Controller: &controller,
			}},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: "busybox"}},
		},
	}
	if _, err := clientset.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create pod: %v", err)
	}

	reg := handlers.NewRegistry()
	rbac.Register(reg)

	deps := &handlers.Deps{
		ClientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return clientset, nil
		},
	}

	handler := reg.Find("find_resource_owners")
	if handler == nil {
		t.Fatal("find_resource_owners tool not found in registry after rbac.Register")
	}

	output, isError := handler(ctx, deps, map[string]interface{}{
		"namespace":     namespace,
		"resource_type": "pods",
	})
	if isError {
		t.Fatalf("find_resource_owners returned an error result: %s", output)
	}
	if !strings.Contains(output, podName) {
		t.Fatalf("find_resource_owners output does not mention pod %q: %s", podName, output)
	}
	if !strings.Contains(output, "Deployment/"+deploymentName) {
		t.Fatalf("find_resource_owners output does not report owner Deployment/%s: %s", deploymentName, output)
	}
}
