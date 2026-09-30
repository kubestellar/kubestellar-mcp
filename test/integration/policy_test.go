//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/handlers"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/policy"
)

// crdEstablishTimeout / crdEstablishInterval bound the poll loop that waits
// for a freshly created CustomResourceDefinition to be served by the
// envtest apiserver. Establishment is asynchronous, so the first dynamic
// request for the new resource can legitimately 404 for a short while.
const (
	crdEstablishTimeout  = 60 * time.Second
	crdEstablishInterval = 250 * time.Millisecond
)

// policyGatekeeperNamespace is the namespace the policy tools hard-code when
// probing for a Gatekeeper installation.
const policyGatekeeperNamespace = "gatekeeper-system"

// constraintTemplateGVR / ownershipConstraintGVR mirror the GVRs the policy
// handlers build internally, so this suite seeds and inspects exactly the
// resources the tools touch.
var (
	constraintTemplateGVR = schema.GroupVersionResource{
		Group:    "templates.gatekeeper.sh",
		Version:  "v1",
		Resource: "constrainttemplates",
	}
	ownershipConstraintGVR = schema.GroupVersionResource{
		Group:    "constraints.gatekeeper.sh",
		Version:  "v1beta1",
		Resource: "k8srequiredlabels",
	}
	crdGVR = schema.GroupVersionResource{
		Group:    "apiextensions.k8s.io",
		Version:  "v1",
		Resource: "customresourcedefinitions",
	}
)

// TestPolicyOwnershipLifecycle exercises pkg/mcp/server/policy's six MCP
// tools end-to-end against a real kube-apiserver (envtest): check_gatekeeper,
// get_ownership_policy_status, list_ownership_violations,
// install_ownership_policy, set_ownership_policy_mode, and
// uninstall_ownership_policy.
//
// Gatekeeper itself is not installed (there is no controller and no webhook
// in envtest), but the two CustomResourceDefinitions its tools read and
// write — ConstraintTemplate and the K8sRequiredLabels constraint — are
// created here with open schemas, which is everything the handlers need:
// they only ever perform dynamic Get/List/Create/Update/Delete on those
// GVRs. That makes the whole install → inspect → change mode → list
// violations → uninstall lifecycle a real apiserver round-trip, including
// the dynamic client's CRD discovery, the "already exists" update branch,
// and the not-found branches before install and after uninstall — none of
// which the package's unit tests (install_branches_test.go,
// set_mode_branches_test.go, status_test.go) can exercise against a fake.
//
// The subtests are deliberately ordered: each one observes the cluster
// state the previous one left behind.
func TestPolicyOwnershipLifecycle(t *testing.T) {
	ctx := context.Background()

	clientset, err := kubernetes.NewForConfig(testCfg)
	require.NoError(t, err, "kubernetes.NewForConfig")

	dynClient, err := dynamic.NewForConfig(testCfg)
	require.NoError(t, err, "dynamic.NewForConfig")

	reg := handlers.NewRegistry()
	policy.Register(reg)

	deps := &handlers.Deps{
		ClientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return clientset, nil
		},
		DynamicClientFactory: func(clusterName string) (dynamic.Interface, error) {
			return dynClient, nil
		},
	}

	call := func(t *testing.T, tool string, args map[string]interface{}) (string, bool) {
		t.Helper()
		handler := reg.Find(tool)
		require.NotNilf(t, handler, "%s tool not found in registry after policy.Register", tool)
		return handler(ctx, deps, args)
	}

	t.Run("check_gatekeeper reports not installed when the namespace is absent", func(t *testing.T) {
		output, isError := call(t, "check_gatekeeper", map[string]interface{}{})
		require.False(t, isError, "check_gatekeeper should not be an error result")
		require.Contains(t, output, "**Status:** Not Installed")
		require.Contains(t, output, "gatekeeper-system` not found")
	})

	seedGatekeeperControlPlane(ctx, t, clientset)
	seedGatekeeperCRDs(ctx, t, dynClient)

	t.Run("check_gatekeeper reports a healthy control plane once its pods are running", func(t *testing.T) {
		output, isError := call(t, "check_gatekeeper", map[string]interface{}{})
		require.False(t, isError, "check_gatekeeper should not be an error result")
		require.Contains(t, output, "**Status:** Installed and Healthy")
		require.Contains(t, output, "**Pods:** 1/1 running")
		require.Contains(t, output, "**ConstraintTemplates:** 0 installed")
		require.Contains(t, output, "**Ownership Policy:** Not installed")
	})

	t.Run("get_ownership_policy_status reports a missing template before install", func(t *testing.T) {
		output, isError := call(t, "get_ownership_policy_status", map[string]interface{}{})
		require.False(t, isError, "get_ownership_policy_status should not be an error result")
		require.Contains(t, output, "**Template:** Not installed")
	})

	t.Run("list_ownership_violations reports a missing policy before install", func(t *testing.T) {
		output, isError := call(t, "list_ownership_violations", map[string]interface{}{})
		require.False(t, isError, "list_ownership_violations should not be an error result")
		require.Contains(t, output, "Ownership policy not installed")
	})

	t.Run("install_ownership_policy creates the template and the constraint", func(t *testing.T) {
		output, isError := call(t, "install_ownership_policy", map[string]interface{}{})
		require.Falsef(t, isError, "install_ownership_policy returned an error result: %s", output)
		require.Contains(t, output, "**ConstraintTemplate:** Created ✓")
		require.Contains(t, output, "**Constraint:** Created ✓")
		require.Contains(t, output, "**Mode:** dryrun")
		require.Contains(t, output, "**Required Labels:** owner, team")

		template, err := dynClient.Resource(constraintTemplateGVR).Get(ctx, "k8srequiredlabels", metav1.GetOptions{})
		require.NoError(t, err, "ConstraintTemplate must exist on the apiserver after install")
		require.Equal(t, "ConstraintTemplate", template.GetKind())

		constraint, err := dynClient.Resource(ownershipConstraintGVR).Get(ctx, "require-ownership-labels", metav1.GetOptions{})
		require.NoError(t, err, "Constraint must exist on the apiserver after install")
		mode, found, err := unstructured.NestedString(constraint.Object, "spec", "enforcementAction")
		require.NoError(t, err, "read spec.enforcementAction")
		require.True(t, found, "spec.enforcementAction must be set by install")
		require.Equal(t, "dryrun", mode)
	})

	t.Run("install_ownership_policy is idempotent and takes the update branch", func(t *testing.T) {
		output, isError := call(t, "install_ownership_policy", map[string]interface{}{
			"mode":   "warn",
			"labels": []interface{}{"owner", "cost-center"},
		})
		require.Falsef(t, isError, "install_ownership_policy returned an error result: %s", output)
		require.Contains(t, output, "**ConstraintTemplate:** Already exists (updating...)")
		require.Contains(t, output, "**Constraint:** Already exists (updating...)")
		require.Contains(t, output, "**Mode:** warn")
		require.Contains(t, output, "**Required Labels:** owner, cost-center")
	})

	t.Run("get_ownership_policy_status reflects the installed constraint", func(t *testing.T) {
		output, isError := call(t, "get_ownership_policy_status", map[string]interface{}{})
		require.False(t, isError, "get_ownership_policy_status should not be an error result")
		require.Contains(t, output, "**Template:** k8srequiredlabels")
		require.Contains(t, output, "**Constraint:** require-ownership-labels")
		require.Contains(t, output, "**Mode:** warn")
		require.Contains(t, output, "**Required Labels:** owner, cost-center")
		require.Contains(t, output, "**Excluded Namespaces:** kube-system")
	})

	t.Run("set_ownership_policy_mode promotes the constraint to enforce", func(t *testing.T) {
		output, isError := call(t, "set_ownership_policy_mode", map[string]interface{}{"mode": "enforce"})
		require.Falsef(t, isError, "set_ownership_policy_mode returned an error result: %s", output)
		require.Contains(t, output, "**Previous Mode:** warn")
		require.Contains(t, output, "**New Mode:** enforce")

		constraint, err := dynClient.Resource(ownershipConstraintGVR).Get(ctx, "require-ownership-labels", metav1.GetOptions{})
		require.NoError(t, err, "Get constraint after set_ownership_policy_mode")
		mode, _, err := unstructured.NestedString(constraint.Object, "spec", "enforcementAction")
		require.NoError(t, err, "read spec.enforcementAction")
		require.Equal(t, "enforce", mode, "the live constraint must carry the new mode")
	})

	t.Run("set_ownership_policy_mode short-circuits when the mode is unchanged", func(t *testing.T) {
		output, isError := call(t, "set_ownership_policy_mode", map[string]interface{}{"mode": "enforce"})
		require.False(t, isError, "set_ownership_policy_mode should not be an error result")
		require.Contains(t, output, "already in `enforce` mode")
	})

	t.Run("set_ownership_policy_mode rejects an unsupported mode", func(t *testing.T) {
		output, isError := call(t, "set_ownership_policy_mode", map[string]interface{}{"mode": "block-everything"})
		require.True(t, isError, "an unsupported mode must be an error result")
		require.Contains(t, output, "mode must be one of: dryrun, warn, enforce")
	})

	seedOwnershipViolations(ctx, t, dynClient)

	t.Run("list_ownership_violations renders the violations recorded on the constraint", func(t *testing.T) {
		output, isError := call(t, "list_ownership_violations", map[string]interface{}{})
		require.False(t, isError, "list_ownership_violations should not be an error result")
		require.Contains(t, output, "**Mode:** enforce")
		require.Contains(t, output, "**Total Violations:** 2")
		require.Contains(t, output, "mcp-integration-policy-alpha")
		require.Contains(t, output, "mcp-integration-policy-beta")
		require.Contains(t, output, "missing required labels")
	})

	t.Run("list_ownership_violations filters violations by namespace", func(t *testing.T) {
		output, isError := call(t, "list_ownership_violations", map[string]interface{}{
			"namespace": "mcp-integration-policy-alpha",
		})
		require.False(t, isError, "list_ownership_violations should not be an error result")
		require.Contains(t, output, "mcp-integration-policy-alpha")
		require.NotContains(t, output, "mcp-integration-policy-beta")
	})

	t.Run("list_ownership_violations reports an empty result for an unmatched namespace", func(t *testing.T) {
		output, isError := call(t, "list_ownership_violations", map[string]interface{}{
			"namespace": "mcp-integration-policy-gamma",
		})
		require.False(t, isError, "list_ownership_violations should not be an error result")
		require.Contains(t, output, "No violations in namespace `mcp-integration-policy-gamma`")
	})

	t.Run("uninstall_ownership_policy deletes the constraint and the template", func(t *testing.T) {
		output, isError := call(t, "uninstall_ownership_policy", map[string]interface{}{})
		require.Falsef(t, isError, "uninstall_ownership_policy returned an error result: %s", output)
		require.Contains(t, output, "**Constraint:** Deleted ✓")
		require.Contains(t, output, "**ConstraintTemplate:** Deleted ✓")

		_, err := dynClient.Resource(ownershipConstraintGVR).Get(ctx, "require-ownership-labels", metav1.GetOptions{})
		require.Error(t, err, "the constraint must be gone after uninstall")

		_, err = dynClient.Resource(constraintTemplateGVR).Get(ctx, "k8srequiredlabels", metav1.GetOptions{})
		require.Error(t, err, "the template must be gone after uninstall")
	})

	t.Run("uninstall_ownership_policy is safe to repeat", func(t *testing.T) {
		output, isError := call(t, "uninstall_ownership_policy", map[string]interface{}{})
		require.Falsef(t, isError, "uninstall_ownership_policy returned an error result: %s", output)
		require.Contains(t, output, "**Constraint:** Not found (already deleted)")
		require.Contains(t, output, "**ConstraintTemplate:** Not found (already deleted)")
	})

	t.Run("get_ownership_policy_status reports a missing template after uninstall", func(t *testing.T) {
		output, isError := call(t, "get_ownership_policy_status", map[string]interface{}{})
		require.False(t, isError, "get_ownership_policy_status should not be an error result")
		require.Contains(t, output, "**Template:** Not installed")
	})
}

// seedGatekeeperControlPlane creates the gatekeeper-system namespace and a
// single Running/Ready pod in it, the shape toolCheckGatekeeper reads when
// deciding between its Healthy, Degraded, and "no pods" branches.
func seedGatekeeperControlPlane(ctx context.Context, t *testing.T, clientset kubernetes.Interface) {
	t.Helper()

	_, err := clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: policyGatekeeperNamespace},
	}, metav1.CreateOptions{})
	require.NoError(t, err, "create gatekeeper-system namespace")

	const podName = "gatekeeper-controller-manager-0"
	created, err := clientset.CoreV1().Pods(policyGatekeeperNamespace).Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: policyGatekeeperNamespace},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "manager", Image: "registry.k8s.io/pause:3.9"},
			},
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err, "create gatekeeper pod")

	created.Status.Phase = corev1.PodRunning
	created.Status.ContainerStatuses = []corev1.ContainerStatus{
		{
			Name:  "manager",
			Ready: true,
			Image: "registry.k8s.io/pause:3.9",
			State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.Now()}},
		},
	}
	_, err = clientset.CoreV1().Pods(policyGatekeeperNamespace).UpdateStatus(ctx, created, metav1.UpdateOptions{})
	require.NoError(t, err, "update gatekeeper pod status")
}

// seedGatekeeperCRDs installs open-schema CustomResourceDefinitions for the
// two Gatekeeper types the policy tools read and write. Gatekeeper normally
// ships these; recreating just their shape keeps the suite free of a
// vendored Gatekeeper manifest while still giving the dynamic client real
// resources to resolve.
func seedGatekeeperCRDs(ctx context.Context, t *testing.T, dynClient dynamic.Interface) {
	t.Helper()

	createOpenCRD(ctx, t, dynClient, "constrainttemplates.templates.gatekeeper.sh",
		constraintTemplateGVR, "ConstraintTemplate", "constrainttemplate")
	createOpenCRD(ctx, t, dynClient, "k8srequiredlabels.constraints.gatekeeper.sh",
		ownershipConstraintGVR, "K8sRequiredLabels", "k8srequiredlabels")
}

// createOpenCRD creates a cluster-scoped CRD for gvr whose schema preserves
// unknown fields (so arbitrary Gatekeeper spec/status content round-trips
// verbatim) and waits until the apiserver actually serves it.
func createOpenCRD(ctx context.Context, t *testing.T, dynClient dynamic.Interface, crdName string, gvr schema.GroupVersionResource, kind, singular string) {
	t.Helper()

	crd := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apiextensions.k8s.io/v1",
			"kind":       "CustomResourceDefinition",
			"metadata": map[string]interface{}{
				"name": crdName,
			},
			"spec": map[string]interface{}{
				"group": gvr.Group,
				"names": map[string]interface{}{
					"kind":     kind,
					"listKind": kind + "List",
					"plural":   gvr.Resource,
					"singular": singular,
				},
				"scope": "Cluster",
				"versions": []interface{}{
					map[string]interface{}{
						"name":    gvr.Version,
						"served":  true,
						"storage": true,
						"schema": map[string]interface{}{
							"openAPIV3Schema": map[string]interface{}{
								"type":                                 "object",
								"x-kubernetes-preserve-unknown-fields": true,
							},
						},
					},
				},
			},
		},
	}

	_, err := dynClient.Resource(crdGVR).Create(ctx, crd, metav1.CreateOptions{})
	require.NoErrorf(t, err, "create CRD %s", crdName)

	deadline := time.Now().Add(crdEstablishTimeout)
	for {
		_, listErr := dynClient.Resource(gvr).List(ctx, metav1.ListOptions{})
		if listErr == nil {
			return
		}
		if time.Now().After(deadline) {
			require.NoErrorf(t, listErr, "CRD %s was not served within %s", crdName, crdEstablishTimeout)
			return
		}
		time.Sleep(crdEstablishInterval)
	}
}

// seedOwnershipViolations writes a status block onto the live constraint
// that looks like the one Gatekeeper's audit loop produces. The constraint
// CRD is created without a /status subresource above precisely so a plain
// Update can seed it here.
func seedOwnershipViolations(ctx context.Context, t *testing.T, dynClient dynamic.Interface) {
	t.Helper()

	const totalViolations = int64(2)

	constraint, err := dynClient.Resource(ownershipConstraintGVR).Get(ctx, "require-ownership-labels", metav1.GetOptions{})
	require.NoError(t, err, "Get constraint before seeding violations")

	violations := []interface{}{
		map[string]interface{}{
			"kind":              "Deployment",
			"name":              "checkout-api",
			"namespace":         "mcp-integration-policy-alpha",
			"message":           "Resource Deployment/checkout-api is missing required labels: {\"owner\"}",
			"enforcementAction": "enforce",
		},
		map[string]interface{}{
			"kind":              "Service",
			"name":              "checkout-svc",
			"namespace":         "mcp-integration-policy-beta",
			"message":           "Resource Service/checkout-svc is missing required labels: {\"cost-center\"}",
			"enforcementAction": "enforce",
		},
	}

	require.NoError(t, unstructured.SetNestedSlice(constraint.Object, violations, "status", "violations"),
		"set status.violations")
	require.NoError(t, unstructured.SetNestedField(constraint.Object, totalViolations, "status", "totalViolations"),
		"set status.totalViolations")

	_, err = dynClient.Resource(ownershipConstraintGVR).Update(ctx, constraint, metav1.UpdateOptions{})
	require.NoError(t, err, "Update constraint with seeded violations")
}
