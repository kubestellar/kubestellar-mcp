// Guards the previously-uncovered branches in
// diagnostics.go:toolGetWarningEvents (83.7% coverage before this file):
//
//   1. extractAndValidateNamespace returns an error   (line 530-532)
//   2. getClientForCluster returns an error           (line 540-542)
//   3. namespace != "" scoped List call               (line 551-553)
//   4. Events.List surfaces an API error              (line 555-557)
//   5. involved_object filter drops non-matching evts (line 564)
//   6. event.LastTimestamp.Time.IsZero() -> "unknown" (line 572)
//   7. args["limit"] float64 propagates to listOpts   (line 534-535)
//
// All existing coverage lives in TestToolGetWarningEvents_NoEvents /
// _HasEvents which only exercise the happy path with a fake client and no
// namespace / filter / error injection.

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

func TestToolGetWarningEvents_InvalidNamespace(t *testing.T) {
	s := &Server{
		clientFactory: func(string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(), nil
		},
	}

	result, isErr := s.toolGetWarningEvents(context.Background(), map[string]interface{}{
		"namespace": "Invalid_NS!",
	})
	if !isErr {
		t.Fatalf("expected isErr=true for invalid namespace, got result=%q", result)
	}
	if !strings.HasPrefix(result, "error:") {
		t.Errorf("expected 'error:' prefix, got %q", result)
	}
}

func TestToolGetWarningEvents_ClientFactoryError(t *testing.T) {
	sentinel := errors.New("kubeconfig missing")
	s := &Server{
		clientFactory: func(string) (kubernetes.Interface, error) {
			return nil, sentinel
		},
	}

	result, isErr := s.toolGetWarningEvents(context.Background(), map[string]interface{}{})
	if !isErr {
		t.Fatalf("expected isErr=true for client factory failure, got %q", result)
	}
	if !strings.Contains(result, "Failed to create client") || !strings.Contains(result, "kubeconfig missing") {
		t.Errorf("unexpected error message: %q", result)
	}
}

func TestToolGetWarningEvents_ListError(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	client.PrependReactor("list", "events", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("etcd unavailable")
	})

	s := &Server{
		clientFactory: func(string) (kubernetes.Interface, error) { return client, nil },
	}

	result, isErr := s.toolGetWarningEvents(context.Background(), map[string]interface{}{})
	if !isErr {
		t.Fatalf("expected isErr=true for events.List failure, got %q", result)
	}
	if !strings.Contains(result, "Failed to list events") || !strings.Contains(result, "etcd unavailable") {
		t.Errorf("unexpected error message: %q", result)
	}
}

func TestToolGetWarningEvents_NamespaceScopedList(t *testing.T) {
	// Namespace-scoped list must exercise the (namespace != "") branch.
	// Two events: one in "kube-system", one in "default"; the "default" one
	// should not appear when we filter on "kube-system".
	client := k8sfake.NewSimpleClientset(
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "ev-ks", Namespace: "app-a"},
			Type:           "Warning",
			Reason:         "SystemAlert",
			Message:        "system-scope-message",
			InvolvedObject: corev1.ObjectReference{Kind: "Node", Name: "node-a"},
			LastTimestamp:  metav1.Now(),
		},
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "ev-def", Namespace: "default"},
			Type:           "Warning",
			Reason:         "AppAlert",
			Message:        "default-scope-message",
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "pod-x"},
			LastTimestamp:  metav1.Now(),
		},
	)

	s := &Server{
		clientFactory: func(string) (kubernetes.Interface, error) { return client, nil },
	}

	result, isErr := s.toolGetWarningEvents(context.Background(), map[string]interface{}{
		"namespace": "app-a",
	})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}
	if !strings.Contains(result, "SystemAlert") {
		t.Errorf("expected app-a event in output: %q", result)
	}
	if strings.Contains(result, "default-scope-message") {
		t.Errorf("default-namespace event leaked into app-a-scoped list: %q", result)
	}
}

func TestToolGetWarningEvents_InvolvedObjectFilter(t *testing.T) {
	// Two events with different InvolvedObject.Name; only the matching one
	// should survive the filter.
	client := k8sfake.NewSimpleClientset(
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "ev-a", Namespace: "default"},
			Type:           "Warning",
			Reason:         "PodOOM",
			Message:        "oom on target pod",
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "target-pod"},
			LastTimestamp:  metav1.Now(),
		},
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "ev-b", Namespace: "default"},
			Type:           "Warning",
			Reason:         "PodOOM",
			Message:        "oom on other pod",
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "other-pod"},
			LastTimestamp:  metav1.Now(),
		},
	)

	s := &Server{
		clientFactory: func(string) (kubernetes.Interface, error) { return client, nil },
	}

	result, isErr := s.toolGetWarningEvents(context.Background(), map[string]interface{}{
		"involved_object": "target-pod",
	})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}
	if !strings.Contains(result, "target-pod") || !strings.Contains(result, "oom on target pod") {
		t.Errorf("expected matching event in output: %q", result)
	}
	if strings.Contains(result, "other-pod") {
		t.Errorf("non-matching involved_object leaked into output: %q", result)
	}
	if !strings.Contains(result, "Found 1 warning events") {
		t.Errorf("expected count=1 header, got %q", result)
	}
}

func TestToolGetWarningEvents_ZeroTimestampShowsUnknownAge(t *testing.T) {
	// A zero LastTimestamp exercises the else-branch that formats "unknown"
	// rather than calling formatAge.
	client := k8sfake.NewSimpleClientset(
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "ev-zero", Namespace: "default"},
			Type:           "Warning",
			Reason:         "NoTime",
			Message:        "clock-skew event",
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "ghost-pod"},
			// LastTimestamp left as the zero value.
		},
	)

	s := &Server{
		clientFactory: func(string) (kubernetes.Interface, error) { return client, nil },
	}

	result, isErr := s.toolGetWarningEvents(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}
	if !strings.Contains(result, "[unknown]") {
		t.Errorf("expected '[unknown]' age tag for zero-timestamp event, got %q", result)
	}
	// event.Count is zero here → the "(occurred N times)" line MUST be
	// absent (guards the Count > 1 branch not being incorrectly taken).
	if strings.Contains(result, "occurred") {
		t.Errorf("unexpected 'occurred N times' line for Count=0 event: %q", result)
	}
}

func TestToolGetWarningEvents_LimitFromArgs(t *testing.T) {
	// args["limit"] is a float64 (JSON numeric) — the parsed value must
	// reach listOpts.Limit unchanged. We verify by intercepting the list
	// call and asserting on the ListOptions carried by the action.
	client := k8sfake.NewSimpleClientset()

	var capturedLimit int64
	client.PrependReactor("list", "events", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if la, ok := action.(k8stesting.ListActionImpl); ok {
			capturedLimit = la.GetListOptions().Limit
		}
		return true, &corev1.EventList{}, nil
	})

	s := &Server{
		clientFactory: func(string) (kubernetes.Interface, error) { return client, nil },
	}

	_, isErr := s.toolGetWarningEvents(context.Background(), map[string]interface{}{
		"limit": float64(7),
	})
	if isErr {
		t.Fatalf("unexpected error path taken with limit override")
	}
	if capturedLimit != 7 {
		t.Errorf("expected listOpts.Limit=7 (from args['limit']=7.0), got %d", capturedLimit)
	}
}
