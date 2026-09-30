//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/diagnostics"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/handlers"
)

// diagnosticsWarningEventCount is the Count recorded on the seeded Warning
// event, chosen >1 so the handler's "(occurred N times)" branch is taken.
const diagnosticsWarningEventCount = 3

// diagnosticsRestartCount is the container restart count seeded on the
// unhealthy pod, chosen >5 so toolFindPodIssues's restart branch fires.
const diagnosticsRestartCount = 7

// diagnosticsDeploymentReplicas / diagnosticsReadyReplicas seed a partially
// rolled out Deployment status (ready < desired) so
// toolFindDeploymentIssues reports both the replica gap and the
// unavailable-replica count.
const (
	diagnosticsDeploymentReplicas = 2
	diagnosticsReadyReplicas      = 1
	diagnosticsUnavailable        = 1
)

// newDiagnosticsDeps builds the handlers.Deps the diagnostics tools consume,
// pointed at the shared envtest apiserver, mirroring workloads_test.go's
// ClientFactory wiring (the same seam the real protocol server fills with a
// kubeconfig-derived client).
func newDiagnosticsDeps(clientset kubernetes.Interface) *handlers.Deps {
	return &handlers.Deps{
		ClientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return clientset, nil
		},
	}
}

// TestDiagnosticsTools exercises every tool in pkg/mcp/server/diagnostics
// end-to-end against a real kube-apiserver (envtest): find_pod_issues,
// find_deployment_issues, check_resource_limits, check_security_issues,
// analyze_namespace, and get_warning_events.
//
// Two Pods (one unhealthy/privileged/limit-less, one healthy and fully
// constrained), one partially-available Deployment, one Service, and one
// Warning Event are seeded directly through the typed clientset — including
// the /status subresource writes envtest's apiserver accepts but
// fake.NewSimpleClientset models loosely — then each tool is dispatched the
// way the real protocol server does it, via diagnostics.Register into a
// handlers.Registry plus handlers.Registry.Find(name), and asserted on the
// exact string a client would receive.
//
// This covers behaviors the package's own unit tests (pod_issues_branches_test.go,
// deployment_issues_branches_test.go, warning_events_branches_test.go, ...)
// can only fake: real status-subresource round-trips, the real
// `type=Warning` field selector on Events, and real cross-resource List
// calls in analyze_namespace.
func TestDiagnosticsTools(t *testing.T) {
	ctx := context.Background()

	clientset, err := kubernetes.NewForConfig(testCfg)
	require.NoError(t, err, "kubernetes.NewForConfig")

	const namespace = "mcp-integration-diagnostics"
	const emptyNamespace = "mcp-integration-diagnostics-empty"
	const unhealthyPodName = "mcp-integration-unhealthy-pod"
	const healthyPodName = "mcp-integration-healthy-pod"
	const deploymentName = "mcp-integration-deployment"
	const serviceName = "mcp-integration-service"

	for _, ns := range []string{namespace, emptyNamespace} {
		_, err := clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: ns},
		}, metav1.CreateOptions{})
		require.NoErrorf(t, err, "create namespace %s", ns)
	}

	seedDiagnosticsUnhealthyPod(ctx, t, clientset, namespace, unhealthyPodName)
	seedDiagnosticsHealthyPod(ctx, t, clientset, namespace, healthyPodName)
	seedDiagnosticsDeployment(ctx, t, clientset, namespace, deploymentName)
	seedDiagnosticsService(ctx, t, clientset, namespace, serviceName)
	seedDiagnosticsWarningEvent(ctx, t, clientset, namespace, unhealthyPodName)

	reg := handlers.NewRegistry()
	diagnostics.Register(reg)
	deps := newDiagnosticsDeps(clientset)

	tests := []struct {
		name           string
		tool           string
		args           map[string]interface{}
		wantContains   []string
		wantNotContain []string
	}{
		{
			name: "find_pod_issues reports the crash-looping pod and skips the healthy one",
			tool: "find_pod_issues",
			args: map[string]interface{}{"namespace": namespace},
			wantContains: []string{
				"Found 1 pods with issues",
				unhealthyPodName,
				"CrashLoopBackOff",
				"has 7 restarts",
			},
			wantNotContain: []string{healthyPodName},
		},
		{
			name:         "find_pod_issues reports a clean namespace as healthy",
			tool:         "find_pod_issues",
			args:         map[string]interface{}{"namespace": emptyNamespace},
			wantContains: []string{"No pod issues found"},
		},
		{
			name: "find_deployment_issues reports the partially available deployment",
			tool: "find_deployment_issues",
			args: map[string]interface{}{"namespace": namespace},
			wantContains: []string{
				"Found 1 deployments with issues",
				deploymentName,
				"Only 1/2 replicas ready",
				"1 replicas unavailable",
			},
		},
		{
			name:         "find_deployment_issues reports a clean namespace as healthy",
			tool:         "find_deployment_issues",
			args:         map[string]interface{}{"namespace": emptyNamespace},
			wantContains: []string{"No deployment issues found"},
		},
		{
			name: "check_resource_limits flags only the container without requests or limits",
			tool: "check_resource_limits",
			args: map[string]interface{}{"namespace": namespace},
			wantContains: []string{
				"Found 1 pods without proper resource limits",
				unhealthyPodName,
				"no CPU limit",
				"no memory request",
			},
			wantNotContain: []string{healthyPodName},
		},
		{
			name: "check_security_issues flags the privileged container",
			tool: "check_security_issues",
			args: map[string]interface{}{"namespace": namespace},
			wantContains: []string{
				"Found 1 pods with security concerns",
				unhealthyPodName,
				"is privileged",
			},
			wantNotContain: []string{healthyPodName},
		},
		{
			name: "analyze_namespace summarizes the seeded workloads",
			tool: "analyze_namespace",
			args: map[string]interface{}{"namespace": namespace},
			wantContains: []string{
				"Namespace Analysis: " + namespace,
				"Status: Active",
				"Total: 2",
				"Running: 2",
				"Deployments: 1",
				"Services: 1",
			},
		},
		{
			name: "get_warning_events returns the seeded warning with its occurrence count",
			tool: "get_warning_events",
			args: map[string]interface{}{"namespace": namespace},
			wantContains: []string{
				"Found 1 warning events",
				"BackOff",
				"Back-off restarting failed container",
				"(occurred 3 times)",
			},
		},
		{
			name:         "get_warning_events filters out events for a different involved object",
			tool:         "get_warning_events",
			args:         map[string]interface{}{"namespace": namespace, "involved_object": "some-other-pod"},
			wantContains: []string{"No warning events found"},
		},
		{
			name:         "get_warning_events reports a clean namespace as warning free",
			tool:         "get_warning_events",
			args:         map[string]interface{}{"namespace": emptyNamespace},
			wantContains: []string{"No warning events found"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := reg.Find(tt.tool)
			require.NotNilf(t, handler, "%s tool not found in registry after diagnostics.Register", tt.tool)

			output, isError := handler(ctx, deps, tt.args)
			require.Falsef(t, isError, "%s returned an error result: %s", tt.tool, output)
			assertDiagnosticsOutputSane(t, tt.tool, output)

			for _, want := range tt.wantContains {
				require.Containsf(t, output, want, "%s output missing %q", tt.tool, want)
			}
			for _, unwanted := range tt.wantNotContain {
				require.NotContainsf(t, output, unwanted, "%s output unexpectedly mentions %q", tt.tool, unwanted)
			}
		})
	}

	t.Run("analyze_namespace requires a namespace argument", func(t *testing.T) {
		handler := reg.Find("analyze_namespace")
		require.NotNil(t, handler, "analyze_namespace tool not found in registry")

		output, isError := handler(ctx, deps, map[string]interface{}{})
		require.True(t, isError, "analyze_namespace without a namespace should be an error result")
		require.Contains(t, output, "namespace is required")
	})

	t.Run("analyze_namespace surfaces the apiserver not-found error", func(t *testing.T) {
		handler := reg.Find("analyze_namespace")
		require.NotNil(t, handler, "analyze_namespace tool not found in registry")

		output, isError := handler(ctx, deps, map[string]interface{}{
			"namespace": "mcp-integration-diagnostics-absent",
		})
		require.True(t, isError, "analyze_namespace on a missing namespace should be an error result")
		require.Contains(t, output, "Failed to get namespace")
		require.Contains(t, output, "not found")
	})
}

// seedDiagnosticsUnhealthyPod creates a pod that trips every diagnostics
// tool at once: no resource requests/limits, a privileged container, and a
// CrashLoopBackOff container status written through the /status subresource
// (envtest has no kubelet, so the status has to be set explicitly).
func seedDiagnosticsUnhealthyPod(ctx context.Context, t *testing.T, clientset kubernetes.Interface, namespace, name string) {
	t.Helper()

	privileged := true
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:            "app",
					Image:           "registry.k8s.io/pause:3.9",
					SecurityContext: &corev1.SecurityContext{Privileged: &privileged},
				},
			},
		},
	}
	created, err := clientset.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	require.NoError(t, err, "create unhealthy pod")

	created.Status.Phase = corev1.PodRunning
	created.Status.ContainerStatuses = []corev1.ContainerStatus{
		{
			Name:         "app",
			Ready:        false,
			RestartCount: diagnosticsRestartCount,
			Image:        "registry.k8s.io/pause:3.9",
			State: corev1.ContainerState{
				Waiting: &corev1.ContainerStateWaiting{
					Reason:  "CrashLoopBackOff",
					Message: "back-off 5m0s restarting failed container=app",
				},
			},
		},
	}
	_, err = clientset.CoreV1().Pods(namespace).UpdateStatus(ctx, created, metav1.UpdateOptions{})
	require.NoError(t, err, "update unhealthy pod status")
}

// seedDiagnosticsHealthyPod creates the control pod: fully constrained
// resources and a hardened security context, so every diagnostics tool must
// leave it out of its findings.
func seedDiagnosticsHealthyPod(ctx context.Context, t *testing.T, clientset kubernetes.Interface, namespace, name string) {
	t.Helper()

	runAsNonRootUser := int64(1000)
	allowPrivilegeEscalation := false
	readOnlyRootFilesystem := true
	containerStarted := true

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "app",
					Image: "registry.k8s.io/pause:3.9",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("50m"),
							corev1.ResourceMemory: resource.MustParse("64Mi"),
						},
					},
					SecurityContext: &corev1.SecurityContext{
						RunAsUser:                &runAsNonRootUser,
						AllowPrivilegeEscalation: &allowPrivilegeEscalation,
						ReadOnlyRootFilesystem:   &readOnlyRootFilesystem,
					},
				},
			},
		},
	}
	created, err := clientset.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	require.NoError(t, err, "create healthy pod")

	created.Status.Phase = corev1.PodRunning
	created.Status.ContainerStatuses = []corev1.ContainerStatus{
		{
			Name:    "app",
			Ready:   true,
			Image:   "registry.k8s.io/pause:3.9",
			State:   corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.Now()}},
			Started: &containerStarted,
		},
	}
	_, err = clientset.CoreV1().Pods(namespace).UpdateStatus(ctx, created, metav1.UpdateOptions{})
	require.NoError(t, err, "update healthy pod status")
}

// seedDiagnosticsDeployment creates a Deployment whose status reports fewer
// ready replicas than desired plus an Available=False condition, the shape
// toolFindDeploymentIssues is built to surface.
func seedDiagnosticsDeployment(ctx context.Context, t *testing.T, clientset kubernetes.Interface, namespace, name string) {
	t.Helper()

	replicas := int32(diagnosticsDeploymentReplicas)
	labels := map[string]string{"app": name}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "app", Image: "registry.k8s.io/pause:3.9"},
					},
				},
			},
		},
	}
	created, err := clientset.AppsV1().Deployments(namespace).Create(ctx, deployment, metav1.CreateOptions{})
	require.NoError(t, err, "create deployment")

	created.Status = appsv1.DeploymentStatus{
		Replicas:            diagnosticsDeploymentReplicas,
		ReadyReplicas:       diagnosticsReadyReplicas,
		AvailableReplicas:   diagnosticsReadyReplicas,
		UnavailableReplicas: diagnosticsUnavailable,
		Conditions: []appsv1.DeploymentCondition{
			{
				Type:    appsv1.DeploymentAvailable,
				Status:  corev1.ConditionFalse,
				Reason:  "MinimumReplicasUnavailable",
				Message: "Deployment does not have minimum availability.",
			},
		},
	}
	_, err = clientset.AppsV1().Deployments(namespace).UpdateStatus(ctx, created, metav1.UpdateOptions{})
	require.NoError(t, err, "update deployment status")
}

// seedDiagnosticsService gives analyze_namespace a Service to count.
func seedDiagnosticsService(ctx context.Context, t *testing.T, clientset kubernetes.Interface, namespace, name string) {
	t.Helper()

	const servicePort = 80

	_, err := clientset.CoreV1().Services(namespace).Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": name},
			Ports:    []corev1.ServicePort{{Port: servicePort}},
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err, "create service")
}

// seedDiagnosticsWarningEvent writes a Warning Event so get_warning_events
// exercises the real `type=Warning` field selector against the apiserver
// rather than the fake clientset's in-memory selector emulation.
func seedDiagnosticsWarningEvent(ctx context.Context, t *testing.T, clientset kubernetes.Interface, namespace, involvedPod string) {
	t.Helper()

	now := metav1.NewTime(time.Now())
	_, err := clientset.CoreV1().Events(namespace).Create(ctx, &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      involvedPod + ".mcp-integration-warning",
			Namespace: namespace,
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:      "Pod",
			Namespace: namespace,
			Name:      involvedPod,
		},
		Reason:         "BackOff",
		Message:        "Back-off restarting failed container",
		Type:           corev1.EventTypeWarning,
		Count:          diagnosticsWarningEventCount,
		FirstTimestamp: now,
		LastTimestamp:  now,
		Source:         corev1.EventSource{Component: "kubelet"},
	}, metav1.CreateOptions{})
	require.NoError(t, err, "create warning event")
}

// assertDiagnosticsOutputSane is a shared guard used by the table above's
// callers: every diagnostics tool must return a non-empty string, because
// the protocol server hands the raw string back to the client verbatim.
func assertDiagnosticsOutputSane(t *testing.T, tool, output string) {
	t.Helper()
	require.NotEmptyf(t, strings.TrimSpace(output), "%s returned an empty result", tool)
}
