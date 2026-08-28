package multicluster

import (
	"context"
	"errors"
	"testing"

	"k8s.io/client-go/kubernetes"
)

// TestConcurrencyLimitDefaultBranch covers the fallback path in
// concurrencyLimit() when maxConcurrency is zero/unset, which was previously
// untested (the only reachable exerciser set maxConcurrency explicitly).
func TestConcurrencyLimitDefaultBranch(t *testing.T) {
	e := &Executor{}
	if got := e.concurrencyLimit(); got != defaultMaxConcurrentClusterOperations {
		t.Fatalf("concurrencyLimit() with zero maxConcurrency = %d, want %d",
			got, defaultMaxConcurrentClusterOperations)
	}

	e.maxConcurrency = -1
	if got := e.concurrencyLimit(); got != defaultMaxConcurrentClusterOperations {
		t.Fatalf("concurrencyLimit() with negative maxConcurrency = %d, want %d",
			got, defaultMaxConcurrentClusterOperations)
	}

	e.maxConcurrency = 7
	if got := e.concurrencyLimit(); got != 7 {
		t.Fatalf("concurrencyLimit() with positive maxConcurrency = %d, want 7", got)
	}
}

// TestExecutorExecuteSingleClusterFnError covers the "fn returned an error"
// branch in executeSingle, which was previously only exercised in multi-cluster
// mode.
func TestExecutorExecuteSingleClusterFnError(t *testing.T) {
	alphaClient := newTestClientset(t)
	executor := NewExecutor(&ClientManager{clients: map[string]*kubernetes.Clientset{"alpha": alphaClient}})

	results, err := executor.Execute(context.Background(), "alpha", func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
		return nil, errors.New("boom")
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Cluster != "alpha" || results[0].Error != "boom" || results[0].Result != nil {
		t.Fatalf("unexpected result: %#v", results[0])
	}
}

// TestExecutorExecuteSingleClusterGetClientError covers the branch where
// GetClient returns an error in single-cluster mode.
func TestExecutorExecuteSingleClusterGetClientError(t *testing.T) {
	// Manager with no clients configured and no kubeconfig on disk: GetClient
	// will fail for "missing".
	manager := &ClientManager{
		clients:    map[string]*kubernetes.Clientset{},
		kubeconfig: "/does/not/exist",
	}
	executor := NewExecutor(manager)

	called := false
	results, err := executor.Execute(context.Background(), "missing", func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
		called = true
		return "should-not-run", nil
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if called {
		t.Fatalf("fn should not have been invoked when GetClient fails")
	}
	if len(results) != 1 || results[0].Cluster != "missing" || results[0].Error == "" {
		t.Fatalf("expected single result with error, got %#v", results)
	}
}
