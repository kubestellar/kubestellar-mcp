package server

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	fake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes"
)

func TestGetClientForClusterWithFactory(t *testing.T) {
	expected := fake.NewSimpleClientset()
	var seenCluster string
	srv := &Server{
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			seenCluster = clusterName
			return expected, nil
		},
	}

	got, err := srv.getClientForCluster("prod-cluster")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != expected {
		t.Fatal("returned client does not match expected")
	}
	if seenCluster != "prod-cluster" {
		t.Fatalf("clusterName passed to factory = %q, want %q", seenCluster, "prod-cluster")
	}
}

func TestGetClientForClusterFactoryError(t *testing.T) {
	sentinel := errors.New("factory boom")
	srv := &Server{
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return nil, sentinel
		},
	}

	got, err := srv.getClientForCluster("any")
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil client on error, got %T", got)
	}
}

func TestGetClientForClusterWithoutFactoryInvalidKubeconfig(t *testing.T) {
	srv := &Server{kubeconfig: "/nonexistent/kubeconfig-should-not-exist"}

	if _, err := srv.getClientForCluster(""); err == nil {
		t.Fatal("expected error with nonexistent kubeconfig (empty clusterName)")
	}
	// Non-empty clusterName exercises the configOverrides.CurrentContext branch.
	if _, err := srv.getClientForCluster("some-cluster"); err == nil {
		t.Fatal("expected error with nonexistent kubeconfig (non-empty clusterName)")
	}
}

func TestGetClientForClusterWithoutFactoryValidKubeconfig(t *testing.T) {
	dir := t.TempDir()
	kubeconfigPath := filepath.Join(dir, "kubeconfig")
	kubeconfig := `apiVersion: v1
kind: Config
current-context: ctx1
clusters:
- name: cluster1
  cluster:
    server: https://127.0.0.1:65535
contexts:
- name: ctx1
  context:
    cluster: cluster1
    user: user1
- name: ctx2
  context:
    cluster: cluster1
    user: user1
users:
- name: user1
  user:
    token: abc
`
	if err := os.WriteFile(kubeconfigPath, []byte(kubeconfig), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	srv := &Server{kubeconfig: kubeconfigPath}

	// Empty clusterName: uses current-context from the file (ctx1).
	client, err := srv.getClientForCluster("")
	if err != nil {
		t.Fatalf("unexpected error with valid kubeconfig / empty cluster: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}

	// Non-empty clusterName: overrides current-context to ctx2 — still resolvable.
	client2, err := srv.getClientForCluster("ctx2")
	if err != nil {
		t.Fatalf("unexpected error with context override: %v", err)
	}
	if client2 == nil {
		t.Fatal("expected non-nil client with override")
	}
}
