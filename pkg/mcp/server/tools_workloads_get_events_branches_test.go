package server

import (
	"context"
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// TestToolGetEventsInvalidNamespaceArg exercises the branch where
// extractAndValidateNamespace returns an error (namespace passed as a non-string).
// The existing TestToolGetEventsSuccess only covers a valid namespace.
func TestToolGetEventsInvalidNamespaceArg(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(), nil
		},
	}

	result, rpcErr := callTool(t, server, "get_events", map[string]interface{}{
		"namespace": 42, // number, not string — triggers extractAndValidateNamespace error
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true for invalid namespace, got success: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "error:") {
		t.Fatalf("expected 'error:' prefix, got: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "namespace must be a string") {
		t.Fatalf("expected 'namespace must be a string' in output, got: %s", result.Content[0].Text)
	}
}

// TestToolGetEventsClientFactoryError exercises the clientFactory error branch:
// s.getClientForCluster returns an error, tool must surface it with the
// "Failed to create client" prefix.
func TestToolGetEventsClientFactoryError(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return nil, fmt.Errorf("kubeconfig context %q not found", clusterName)
		},
	}

	result, rpcErr := callTool(t, server, "get_events", map[string]interface{}{
		"cluster":   "missing-cluster",
		"namespace": "apps",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true on clientFactory failure, got success: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "Failed to create client") {
		t.Fatalf("expected 'Failed to create client' prefix, got: %s", result.Content[0].Text)
	}
}

// TestToolGetEventsListError exercises the events.List error branch:
// the fake client's list reactor returns an error, tool must surface it with
// "Failed to list events" prefix.
func TestToolGetEventsListError(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	client.PrependReactor("list", "events", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("etcd unavailable")
	})
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, rpcErr := callTool(t, server, "get_events", map[string]interface{}{"namespace": "apps"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true on list error, got success: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "Failed to list events") {
		t.Fatalf("expected 'Failed to list events' prefix, got: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "etcd unavailable") {
		t.Fatalf("expected underlying error to be surfaced, got: %s", result.Content[0].Text)
	}
}

// TestToolGetEventsAllNamespaces exercises the empty-namespace branch that
// lists events across all namespaces. Without a `namespace` argument the tool
// must call Events("").List, which the fake client-set handles by returning
// events regardless of their namespace field.
func TestToolGetEventsAllNamespaces(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(
				&corev1.Event{
					ObjectMeta:     metav1.ObjectMeta{Name: "evt-ns1", Namespace: "ns1"},
					Type:           "Warning",
					Message:        "OOM killed",
					InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "worker-1"},
				},
				&corev1.Event{
					ObjectMeta:     metav1.ObjectMeta{Name: "evt-ns2", Namespace: "ns2"},
					Type:           "Normal",
					Message:        "Scaled up",
					InvolvedObject: corev1.ObjectReference{Kind: "Deployment", Name: "api"},
				},
			), nil
		},
	}

	// Omit "namespace" entirely — this is the all-namespaces branch.
	result, rpcErr := callTool(t, server, "get_events", map[string]interface{}{})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "Found 2 events") {
		t.Fatalf("expected 'Found 2 events' across all namespaces, got: %s", text)
	}
	if !strings.Contains(text, "Pod/worker-1") || !strings.Contains(text, "Deployment/api") {
		t.Fatalf("expected both cross-namespace events in output, got: %s", text)
	}
}

// TestToolGetEventsEmptyList exercises the "No events found" branch:
// the API returns an empty EventList and the tool returns the sentinel string
// with IsError=false. Combined with the success branch this pins down the
// len(events.Items) == 0 vs. > 0 fork.
func TestToolGetEventsEmptyList(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(), nil
		},
	}

	result, rpcErr := callTool(t, server, "get_events", map[string]interface{}{"namespace": "apps"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected IsError=false for empty list, got error: %s", result.Content[0].Text)
	}
	if result.Content[0].Text != "No events found" {
		t.Fatalf("expected exactly 'No events found', got: %s", result.Content[0].Text)
	}
}

// TestToolGetEventsLimitOverride exercises the `limit` float64 override
// branch. Without a way to inspect ListOptions.Limit through the fake
// client-set's action interface, we verify indirectly: the tool must accept
// the arg without error and still return events. This pins down the
// `if v, ok := args["limit"].(float64); ok` conversion branch.
func TestToolGetEventsLimitOverride(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(
				&corev1.Event{
					ObjectMeta:     metav1.ObjectMeta{Name: "evt-1", Namespace: "apps"},
					Type:           "Warning",
					Message:        "test",
					InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "p"},
				},
			), nil
		},
	}

	result, rpcErr := callTool(t, server, "get_events", map[string]interface{}{
		"namespace": "apps",
		"limit":     float64(7),
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success with limit override, got error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "Found 1 events") {
		t.Fatalf("expected 'Found 1 events' with limit override, got: %s", result.Content[0].Text)
	}
}

// Compile-time guard so unused-import warnings don't creep in if the file
// ever loses one of its subject-under-test call sites.
var _ = context.Background
var _ = k8stesting.NewRootListAction
