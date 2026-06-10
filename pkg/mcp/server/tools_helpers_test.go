package server

import (
	"testing"

	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/rest"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestGetDynamicClientForClusterWithFactory(t *testing.T) {
	expectedClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	server := &Server{
		dynamicClientFactory: func(clusterName string) (dynamic.Interface, error) {
			if clusterName != "test-cluster" {
				t.Errorf("expected cluster 'test-cluster', got %q", clusterName)
			}
			return expectedClient, nil
		},
	}

	client, err := server.getDynamicClientForCluster("test-cluster")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client != expectedClient {
		t.Error("returned client does not match expected")
	}
}

func TestGetDynamicClientForClusterWithoutFactory(t *testing.T) {
	// Without a factory and with an invalid kubeconfig, we expect an error
	server := &Server{
		kubeconfig: "/nonexistent/kubeconfig",
	}

	_, err := server.getDynamicClientForCluster("test-cluster")
	if err == nil {
		t.Fatal("expected error with nonexistent kubeconfig")
	}
}

func TestGetRestConfigForClusterWithFactory(t *testing.T) {
	expectedConfig := &rest.Config{Host: "https://test-server:6443"}
	server := &Server{
		restConfigFactory: func(clusterName string) (*rest.Config, error) {
			if clusterName != "my-cluster" {
				t.Errorf("expected cluster 'my-cluster', got %q", clusterName)
			}
			return expectedConfig, nil
		},
	}

	config, err := server.getRestConfigForCluster("my-cluster")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config.Host != "https://test-server:6443" {
		t.Errorf("expected host 'https://test-server:6443', got %q", config.Host)
	}
}

func TestGetRestConfigForClusterWithoutFactory(t *testing.T) {
	// Without a factory and with an invalid kubeconfig, we expect an error
	server := &Server{
		kubeconfig: "/nonexistent/kubeconfig",
	}

	_, err := server.getRestConfigForCluster("test-cluster")
	if err == nil {
		t.Fatal("expected error with nonexistent kubeconfig")
	}
}
