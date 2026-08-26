package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// applyBranchCase represents a scenario for exercising the created / unchanged /
// error branches of the apply{Deployment,Service,ConfigMap,Secret} helpers.
type applyBranchCase struct {
	name      string
	resource  string
	raw       map[string]interface{}
	applyFunc func(*Server, context.Context, kubernetes.Interface, map[string]interface{}, string) (string, error)
	makeObj   func(rv string) runtime.Object
}

func applyBranchCases() []applyBranchCase {
	rawDeployment := map[string]interface{}{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]interface{}{"name": "demo"},
	}
	rawService := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata":   map[string]interface{}{"name": "demo"},
	}
	rawConfigMap := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]interface{}{"name": "demo"},
	}
	rawSecret := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata":   map[string]interface{}{"name": "demo"},
	}

	return []applyBranchCase{
		{
			name:     "deployment",
			resource: "deployments",
			raw:      rawDeployment,
			applyFunc: func(s *Server, ctx context.Context, c kubernetes.Interface, raw map[string]interface{}, ns string) (string, error) {
				return s.applyDeployment(ctx, c, raw, ns)
			},
			makeObj: func(rv string) runtime.Object {
				return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default", ResourceVersion: rv}}
			},
		},
		{
			name:     "service",
			resource: "services",
			raw:      rawService,
			applyFunc: func(s *Server, ctx context.Context, c kubernetes.Interface, raw map[string]interface{}, ns string) (string, error) {
				return s.applyService(ctx, c, raw, ns)
			},
			makeObj: func(rv string) runtime.Object {
				return &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default", ResourceVersion: rv}}
			},
		},
		{
			name:     "configmap",
			resource: "configmaps",
			raw:      rawConfigMap,
			applyFunc: func(s *Server, ctx context.Context, c kubernetes.Interface, raw map[string]interface{}, ns string) (string, error) {
				return s.applyConfigMap(ctx, c, raw, ns)
			},
			makeObj: func(rv string) runtime.Object {
				return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default", ResourceVersion: rv}}
			},
		},
		{
			name:     "secret",
			resource: "secrets",
			raw:      rawSecret,
			applyFunc: func(s *Server, ctx context.Context, c kubernetes.Interface, raw map[string]interface{}, ns string) (string, error) {
				return s.applySecret(ctx, c, raw, ns)
			},
			makeObj: func(rv string) runtime.Object {
				return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default", ResourceVersion: rv}}
			},
		},
	}
}

// TestApplyResourceFunctions_NewResource_ReturnsUpdatedNotCreated documents
// current behavior of the apply* helpers: when the target resource does not
// exist, they report "updated" rather than "created".
//
// The root cause is a subtle client-go interaction. The typed Get() methods
// (e.g. Deployments().Get) allocate a fresh empty struct with `result = &T{}`
// and return that pointer alongside the NotFound error. As a result, the
// `if existing == nil { return "created" }` branch in apply* is dead code —
// existing is always a non-nil empty struct on NotFound, with ResourceVersion
// == "", so the final branch compares "" against the patched RV and reports
// "updated". This is a UX bug (misleading status string) filed as an advisory
// bead. This test guards against silent behavior changes and will need to be
// updated when the bug is fixed to expect "created".
func TestApplyResourceFunctions_NewResource_ReturnsUpdatedNotCreated(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})
	for _, tc := range applyBranchCases() {
		t.Run(tc.name, func(t *testing.T) {
			client := kubernetesfake.NewSimpleClientset()
			// Force Get to return NotFound; typed client will still hand
			// back a non-nil empty struct along with the error.
			client.PrependReactor("get", tc.resource, func(action k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, apierrors.NewNotFound(schema.GroupResource{Resource: tc.resource}, "demo")
			})
			client.PrependReactor("patch", tc.resource, func(action k8stesting.Action) (bool, runtime.Object, error) {
				return true, tc.makeObj("1"), nil
			})

			status, err := tc.applyFunc(server, context.Background(), client, tc.raw, "default")
			require.NoError(t, err)
			assert.Equal(t, "updated", status,
				"documents current misreporting: NotFound Get should ideally yield \"created\"; see advisory bead")
		})
	}
}

// TestApplyResourceFunctions_UnchangedBranch verifies each apply* function
// returns "unchanged" when the patch response has the same ResourceVersion as
// the existing object, meaning server-side apply produced no diff.
func TestApplyResourceFunctions_UnchangedBranch(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})
	for _, tc := range applyBranchCases() {
		t.Run(tc.name, func(t *testing.T) {
			existing := tc.makeObj("42")
			client := kubernetesfake.NewSimpleClientset(existing)
			client.PrependReactor("patch", tc.resource, func(action k8stesting.Action) (bool, runtime.Object, error) {
				// Return same ResourceVersion -> unchanged
				return true, tc.makeObj("42"), nil
			})

			status, err := tc.applyFunc(server, context.Background(), client, tc.raw, "default")
			require.NoError(t, err)
			assert.Equal(t, "unchanged", status)
		})
	}
}

// TestApplyResourceFunctions_GetErrorPropagates verifies that a non-NotFound
// error from the Get call propagates as an error (e.g. transient API server
// failures should not be swallowed as "created").
func TestApplyResourceFunctions_GetErrorPropagates(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})
	for _, tc := range applyBranchCases() {
		t.Run(tc.name, func(t *testing.T) {
			client := kubernetesfake.NewSimpleClientset()
			boom := errors.New("transient api server failure")
			client.PrependReactor("get", tc.resource, func(action k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, boom
			})

			_, err := tc.applyFunc(server, context.Background(), client, tc.raw, "default")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "transient api server failure")
		})
	}
}

// TestApplyResourceFunctions_PatchErrorPropagates verifies that an error from
// the Patch call is surfaced to the caller.
func TestApplyResourceFunctions_PatchErrorPropagates(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})
	for _, tc := range applyBranchCases() {
		t.Run(tc.name, func(t *testing.T) {
			client := kubernetesfake.NewSimpleClientset()
			// Get returns NotFound so we hit the patch branch.
			client.PrependReactor("get", tc.resource, func(action k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, apierrors.NewNotFound(schema.GroupResource{Resource: tc.resource}, "demo")
			})
			client.PrependReactor("patch", tc.resource, func(action k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, errors.New("patch rejected by admission webhook")
			})

			_, err := tc.applyFunc(server, context.Background(), client, tc.raw, "default")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "patch rejected by admission webhook")
		})
	}
}

// TestApplyResourceFunctions_UnmarshalErrorPropagates verifies that a raw
// object whose shape does not match the target typed object surfaces the
// JSON unmarshal error rather than silently producing an empty object.
// A non-string value in a string field (e.g. metadata.name as a number)
// triggers json.Unmarshal to fail.
func TestApplyResourceFunctions_UnmarshalErrorPropagates(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})
	badRaw := map[string]interface{}{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		// metadata should be an object; supplying a scalar makes typed
		// unmarshal fail deterministically for all target kinds.
		"metadata": "not-an-object",
	}
	for _, tc := range applyBranchCases() {
		t.Run(tc.name, func(t *testing.T) {
			client := kubernetesfake.NewSimpleClientset()
			_, err := tc.applyFunc(server, context.Background(), client, badRaw, "default")
			require.Error(t, err)
		})
	}
}

// TestApplyResourceFunctions_NamespaceDefaulting verifies that when the raw
// object omits metadata.namespace, the caller's namespace argument is used
// on the Patch call (i.e. the defaulting branch runs).
func TestApplyResourceFunctions_NamespaceDefaulting(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})
	for _, tc := range applyBranchCases() {
		t.Run(tc.name, func(t *testing.T) {
			client := kubernetesfake.NewSimpleClientset()
			var patchedNS string
			client.PrependReactor("patch", tc.resource, func(action k8stesting.Action) (bool, runtime.Object, error) {
				patchedNS = action.GetNamespace()
				return true, tc.makeObj("1"), nil
			})

			_, err := tc.applyFunc(server, context.Background(), client, tc.raw, "kube-system")
			require.NoError(t, err)
			assert.Equal(t, "kube-system", patchedNS, "namespace argument should be applied to patch when raw omits it")
		})
	}
}
