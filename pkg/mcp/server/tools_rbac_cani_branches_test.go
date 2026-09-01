package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// Existing tests (TestToolCanIAllowed / TestToolCanIValidation) cover
// happy-path formatting and the missing-verb/-resource validation branches,
// leaving toolCanI (tools_rbac.go:190) at 82.5% statement coverage. This
// file exercises the remaining branches.

// TestToolCanI_InvalidNamespaceArg covers the extractAndValidateNamespace
// error branch inside toolCanI.
func TestToolCanI_InvalidNamespaceArg(t *testing.T) {
	s := &Server{
		clientFactory: func(string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(), nil
		},
	}

	result, isErr := s.toolCanI(context.Background(), map[string]interface{}{
		"verb":      "get",
		"resource":  "pods",
		"namespace": "Invalid_NS!",
	})
	if !isErr {
		t.Fatalf("expected error for invalid namespace, got: %s", result)
	}
	if !strings.HasPrefix(result, "error:") {
		t.Errorf("expected 'error:' prefix, got: %s", result)
	}
}

// TestToolCanI_ClientFactoryError covers the getClientForCluster failure
// branch.
func TestToolCanI_ClientFactoryError(t *testing.T) {
	s := &Server{
		clientFactory: func(string) (kubernetes.Interface, error) {
			return nil, errors.New("kubeconfig missing")
		},
	}

	result, isErr := s.toolCanI(context.Background(), map[string]interface{}{
		"verb":     "get",
		"resource": "pods",
	})
	if !isErr {
		t.Fatalf("expected error when clientFactory fails, got: %s", result)
	}
	if !strings.Contains(result, "Failed to create client") {
		t.Errorf("expected 'Failed to create client', got: %s", result)
	}
}

// TestToolCanI_SARCreateError covers the SelfSubjectAccessReview.Create
// failure branch. We use a reactor to force the fake client to return an
// error on the SAR create call.
func TestToolCanI_SARCreateError(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	client.PrependReactor("create", "selfsubjectaccessreviews",
		func(action k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("api-server unreachable")
		})

	s := &Server{
		clientFactory: func(string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolCanI(context.Background(), map[string]interface{}{
		"verb":     "get",
		"resource": "pods",
	})
	if !isErr {
		t.Fatalf("expected error from SAR create, got: %s", result)
	}
	if !strings.Contains(result, "Failed to check access") {
		t.Errorf("expected 'Failed to check access', got: %s", result)
	}
}

// TestToolCanI_AllowedTruePath covers the `result.Status.Allowed == true`
// branch, which is the positive-permission formatting path.
func TestToolCanI_AllowedTruePath(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	client.PrependReactor("create", "selfsubjectaccessreviews",
		func(action k8stesting.Action) (bool, runtime.Object, error) {
			return true, &authorizationv1.SelfSubjectAccessReview{
				Status: authorizationv1.SubjectAccessReviewStatus{Allowed: true},
			}, nil
		})

	s := &Server{
		clientFactory: func(string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolCanI(context.Background(), map[string]interface{}{
		"verb":     "list",
		"resource": "configmaps",
	})
	if isErr {
		t.Fatalf("expected success, got error: %s", result)
	}
	if !strings.Contains(result, "Yes, access is allowed") {
		t.Errorf("expected allowed marker, got: %s", result)
	}
}

// TestToolCanI_DeniedWithReason covers the `result.Status.Reason != ""`
// branch on a denial response — the tool must surface the reason string.
func TestToolCanI_DeniedWithReason(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	client.PrependReactor("create", "selfsubjectaccessreviews",
		func(action k8stesting.Action) (bool, runtime.Object, error) {
			return true, &authorizationv1.SelfSubjectAccessReview{
				Status: authorizationv1.SubjectAccessReviewStatus{
					Allowed: false,
					Reason:  "policy 'read-only' denies write verbs",
				},
			}, nil
		})

	s := &Server{
		clientFactory: func(string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolCanI(context.Background(), map[string]interface{}{
		"verb":     "delete",
		"resource": "secrets",
	})
	if isErr {
		t.Fatalf("expected success (denied is not an rpc error), got: %s", result)
	}
	if !strings.Contains(result, "No, access is denied") {
		t.Errorf("expected denied marker, got: %s", result)
	}
	if !strings.Contains(result, "policy 'read-only' denies write verbs") {
		t.Errorf("expected reason text, got: %s", result)
	}
}

// TestToolCanI_SubresourceAndNameFormatting covers the two Fprintf branches
// that fire only when the caller supplies `subresource` and `name`.
func TestToolCanI_SubresourceAndNameFormatting(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	// Leave the default fake behaviour: SAR create returns allowed=false,
	// no reason set. The `Allowed` and `Reason` branches are not the
	// subject of this test — the formatting branches are.
	_ = metav1.ListOptions{}

	s := &Server{
		clientFactory: func(string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolCanI(context.Background(), map[string]interface{}{
		"verb":        "get",
		"resource":    "pods",
		"subresource": "log",
		"name":        "app-1",
		"namespace":   "default",
	})
	if isErr {
		t.Fatalf("expected success, got: %s", result)
	}
	if !strings.Contains(result, "pods/log") {
		t.Errorf("expected 'pods/log' subresource formatting, got: %s", result)
	}
	if !strings.Contains(result, "(name: app-1)") {
		t.Errorf("expected '(name: app-1)' formatting, got: %s", result)
	}
	if !strings.Contains(result, "in namespace default") {
		t.Errorf("expected namespace formatting, got: %s", result)
	}
}
