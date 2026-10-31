package handlers

import (
	"errors"
	"testing"

	k8sfake "k8s.io/client-go/kubernetes/fake"

	"k8s.io/client-go/kubernetes"
)

func TestResolveClusterClient_NamespaceError(t *testing.T) {
	d := &Deps{ClientFactory: func(string) (kubernetes.Interface, error) {
		t.Fatal("client factory should not be called when namespace validation fails")
		return nil, nil
	}}

	client, cluster, namespace, errMsg, isErr := d.ResolveClusterClient(map[string]interface{}{
		"cluster":   "any",
		"namespace": 42,
	})
	if !isErr {
		t.Fatal("expected isErr = true")
	}
	if client != nil {
		t.Fatalf("expected nil client, got %v", client)
	}
	if cluster != "any" {
		t.Fatalf("expected cluster to still be extracted, got %q", cluster)
	}
	if namespace != "" {
		t.Fatalf("expected empty namespace on error, got %q", namespace)
	}
	if errMsg == "" {
		t.Fatal("expected a non-empty errMsg")
	}
}

func TestResolveClusterClient_ClientError(t *testing.T) {
	sentinel := errors.New("client boom")
	d := &Deps{ClientFactory: func(string) (kubernetes.Interface, error) { return nil, sentinel }}

	client, _, _, errMsg, isErr := d.ResolveClusterClient(map[string]interface{}{"cluster": "any"})
	if !isErr {
		t.Fatal("expected isErr = true")
	}
	if client != nil {
		t.Fatalf("expected nil client, got %v", client)
	}
	if errMsg != "Failed to create client: client boom" {
		t.Fatalf("unexpected errMsg: %q", errMsg)
	}
}

func TestResolveClusterClient_Success(t *testing.T) {
	fakeClient := k8sfake.NewSimpleClientset()
	d := &Deps{ClientFactory: func(string) (kubernetes.Interface, error) { return fakeClient, nil }}

	client, cluster, namespace, errMsg, isErr := d.ResolveClusterClient(map[string]interface{}{
		"cluster":   "my-cluster",
		"namespace": "team-a",
	})
	if isErr {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if client != fakeClient {
		t.Fatalf("expected the fake client to be returned, got %v", client)
	}
	if cluster != "my-cluster" {
		t.Fatalf("unexpected cluster: %q", cluster)
	}
	if namespace != "team-a" {
		t.Fatalf("unexpected namespace: %q", namespace)
	}
	if errMsg != "" {
		t.Fatalf("expected empty errMsg, got %q", errMsg)
	}
}
