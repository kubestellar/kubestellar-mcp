package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// The existing diagnostics_test.go covers the happy path plus NotReady and
// ProgressingFalse for toolFindDeploymentIssues. This file adds the remaining
// six branches identified in kubestellar/kubestellar-mcp#652, taking the
// function from 72.9% line coverage toward the ~90% floor the rest of the
// package already meets.
//
// Every test follows the established fake-client + Server{clientFactory}
// pattern used by the sibling tests in this package.

func TestToolFindDeploymentIssues_AvailableFalse(t *testing.T) {
	client := k8sfake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "unavailable-deploy", Namespace: "default"},
		Status: appsv1.DeploymentStatus{
			Replicas:      3,
			ReadyReplicas: 3,
			Conditions: []appsv1.DeploymentCondition{
				{
					Type:    appsv1.DeploymentAvailable,
					Status:  corev1.ConditionFalse,
					Message: "Deployment does not have minimum availability.",
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

	for _, want := range []string{"unavailable-deploy", "Not available", "minimum availability"} {
		if !strings.Contains(result, want) {
			t.Errorf("toolFindDeploymentIssues() missing %q in:\n%s", want, result)
		}
	}
}

func TestToolFindDeploymentIssues_ReplicaFailure(t *testing.T) {
	client := k8sfake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "quota-blocked", Namespace: "default"},
		Status: appsv1.DeploymentStatus{
			Replicas:      3,
			ReadyReplicas: 3,
			Conditions: []appsv1.DeploymentCondition{
				{
					Type:    appsv1.DeploymentReplicaFailure,
					Status:  corev1.ConditionTrue,
					Message: "pods \"web-abc\" is forbidden: exceeded quota",
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

	for _, want := range []string{"quota-blocked", "Replica failure", "exceeded quota"} {
		if !strings.Contains(result, want) {
			t.Errorf("toolFindDeploymentIssues() missing %q in:\n%s", want, result)
		}
	}
}

func TestToolFindDeploymentIssues_ReplicaSetError(t *testing.T) {
	// Build a Deployment plus TWO owned ReplicaSets so we also exercise the
	// "latest ReplicaSet wins" tie-breaker in the rsMap build loop. The
	// newer RS carries the ReplicaSetReplicaFailure condition; the older RS
	// has no conditions and must NOT contribute to the output.
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "rs-owner", Namespace: "default", UID: "deploy-uid-1"},
		Status: appsv1.DeploymentStatus{
			Replicas:      3,
			ReadyReplicas: 3,
		},
	}
	oldTS := metav1.Now()
	newerTS := metav1.NewTime(oldTS.Add(60_000_000_000)) // +60s
	oldRS := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "rs-owner-old", Namespace: "default",
			CreationTimestamp: oldTS,
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "Deployment", Name: "rs-owner", UID: "deploy-uid-1"},
			},
		},
	}
	newRS := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "rs-owner-new", Namespace: "default",
			CreationTimestamp: newerTS,
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "Deployment", Name: "rs-owner", UID: "deploy-uid-1"},
			},
		},
		Status: appsv1.ReplicaSetStatus{
			Conditions: []appsv1.ReplicaSetCondition{
				{
					Type:    appsv1.ReplicaSetReplicaFailure,
					Status:  corev1.ConditionTrue,
					Message: "pods create failed: node not ready",
				},
			},
		},
	}

	client := k8sfake.NewSimpleClientset(deploy, oldRS, newRS)
	s := &Server{
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolFindDeploymentIssues(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("toolFindDeploymentIssues() returned error: %s", result)
	}

	for _, want := range []string{"rs-owner", "ReplicaSet error", "node not ready"} {
		if !strings.Contains(result, want) {
			t.Errorf("toolFindDeploymentIssues() missing %q in:\n%s", want, result)
		}
	}
}

func TestToolFindDeploymentIssues_AllNamespaces(t *testing.T) {
	// Two deployments in distinct namespaces; both have issues. Calling
	// without a namespace arg must list across all namespaces and surface
	// both.
	client := k8sfake.NewSimpleClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "ns-a-deploy", Namespace: "ns-a"},
			Status: appsv1.DeploymentStatus{
				Replicas: 2, ReadyReplicas: 0, UnavailableReplicas: 2,
			},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "ns-b-deploy", Namespace: "ns-b"},
			Status: appsv1.DeploymentStatus{
				Replicas: 4, ReadyReplicas: 1, UnavailableReplicas: 3,
			},
		},
	)

	s := &Server{
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolFindDeploymentIssues(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("toolFindDeploymentIssues() returned error: %s", result)
	}

	for _, want := range []string{"ns-a-deploy", "ns-b-deploy"} {
		if !strings.Contains(result, want) {
			t.Errorf("toolFindDeploymentIssues() all-namespaces mode missing %q in:\n%s", want, result)
		}
	}
}

func TestToolFindDeploymentIssues_ListError(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	client.PrependReactor("list", "deployments",
		func(action k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("boom: apiserver unavailable")
		},
	)

	s := &Server{
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, isErr := s.toolFindDeploymentIssues(context.Background(), map[string]interface{}{})
	if !isErr {
		t.Fatalf("toolFindDeploymentIssues() expected isErr=true on list error, got isErr=false, result=%q", result)
	}
	for _, want := range []string{"Failed to list deployments", "apiserver unavailable"} {
		if !strings.Contains(result, want) {
			t.Errorf("toolFindDeploymentIssues() list-error missing %q in:\n%s", want, result)
		}
	}
}

func TestToolFindDeploymentIssues_ClientFactoryError(t *testing.T) {
	s := &Server{
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return nil, errors.New("cluster \"nope\" not found in kubeconfig")
		},
	}

	result, isErr := s.toolFindDeploymentIssues(context.Background(), map[string]interface{}{
		"cluster": "nope",
	})
	if !isErr {
		t.Fatalf("toolFindDeploymentIssues() expected isErr=true on client-factory error, got isErr=false, result=%q", result)
	}
	for _, want := range []string{"Failed to create client", "not found in kubeconfig"} {
		if !strings.Contains(result, want) {
			t.Errorf("toolFindDeploymentIssues() client-factory-error missing %q in:\n%s", want, result)
		}
	}
}
