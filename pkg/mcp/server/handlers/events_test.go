package handlers

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestListEventsForCluster_ClientError(t *testing.T) {
	sentinel := errors.New("client boom")
	d := &Deps{ClientFactory: func(string) (kubernetes.Interface, error) { return nil, sentinel }}

	events, errMsg, isErr := d.ListEventsForCluster(context.Background(), "any", "", metav1.ListOptions{})
	if !isErr {
		t.Fatal("expected isErr = true")
	}
	if events != nil {
		t.Fatalf("expected nil events, got %v", events)
	}
	if errMsg != "Failed to create client: client boom" {
		t.Fatalf("unexpected errMsg: %q", errMsg)
	}
}

func TestListEventsForCluster_ListError(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	client.PrependReactor("list", "events", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("list boom")
	})
	d := &Deps{ClientFactory: func(string) (kubernetes.Interface, error) { return client, nil }}

	events, errMsg, isErr := d.ListEventsForCluster(context.Background(), "any", "", metav1.ListOptions{})
	if !isErr {
		t.Fatal("expected isErr = true")
	}
	if events != nil {
		t.Fatalf("expected nil events, got %v", events)
	}
	if errMsg != "Failed to list events: list boom" {
		t.Fatalf("unexpected errMsg: %q", errMsg)
	}
}

func TestListEventsForCluster_ClusterWide(t *testing.T) {
	existing := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{Name: "ev1", Namespace: "default"},
		Message:    "something happened",
	}
	client := k8sfake.NewSimpleClientset(existing)
	d := &Deps{ClientFactory: func(string) (kubernetes.Interface, error) { return client, nil }}

	events, errMsg, isErr := d.ListEventsForCluster(context.Background(), "any", "", metav1.ListOptions{})
	if isErr {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if len(events.Items) != 1 || events.Items[0].Name != "ev1" {
		t.Fatalf("expected the single cluster-wide event, got %+v", events.Items)
	}
}

func TestListEventsForCluster_NamespaceScoped(t *testing.T) {
	inNS := &corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "ev-ns", Namespace: "team-a"}}
	otherNS := &corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "ev-other", Namespace: "team-b"}}
	client := k8sfake.NewSimpleClientset(inNS, otherNS)
	d := &Deps{ClientFactory: func(string) (kubernetes.Interface, error) { return client, nil }}

	events, errMsg, isErr := d.ListEventsForCluster(context.Background(), "any", "team-a", metav1.ListOptions{})
	if isErr {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if len(events.Items) != 1 || events.Items[0].Name != "ev-ns" {
		t.Fatalf("expected only the team-a event, got %+v", events.Items)
	}
}
