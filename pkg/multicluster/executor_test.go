package multicluster

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestExecutorExecuteSingleCluster(t *testing.T) {
	alphaClient := newTestClientset(t)
	executor := NewExecutor(&ClientManager{clients: map[string]*kubernetes.Clientset{"alpha": alphaClient}})

	results, err := executor.Execute(context.Background(), "alpha", func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
		if client != alphaClient {
			t.Fatal("executor passed unexpected client")
		}
		if clusterName != "alpha" {
			t.Fatalf("clusterName = %q, want alpha", clusterName)
		}
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(results) != 1 || results[0].Cluster != "alpha" || results[0].Result != "ok" || results[0].Error != "" {
		t.Fatalf("unexpected results: %#v", results)
	}
}

func TestExecutorExecuteAllRunsInParallel(t *testing.T) {
	manager := newTestManager(t, []string{"alpha", "beta", "gamma"})
	executor := NewExecutor(manager)

	started := int32(0)
	release := make(chan struct{})
	resultCh := make(chan []ClusterResult, 1)
	errCh := make(chan error, 1)

	go func() {
		results, err := executor.Execute(context.Background(), "", func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
			atomic.AddInt32(&started, 1)
			<-release
			return clusterName + "-done", nil
		})
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- results
	}()

	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&started) < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for parallel workers, started=%d", atomic.LoadInt32(&started))
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(release)

	select {
	case err := <-errCh:
		t.Fatalf("Execute() error = %v", err)
	case results := <-resultCh:
		if len(results) != 3 {
			t.Fatalf("result count = %d, want 3", len(results))
		}
		gotClusters := []string{results[0].Cluster, results[1].Cluster, results[2].Cluster}
		sort.Strings(gotClusters)
		if fmt.Sprint(gotClusters) != fmt.Sprint([]string{"alpha", "beta", "gamma"}) {
			t.Fatalf("clusters = %v, want [alpha beta gamma]", gotClusters)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Execute() to finish")
	}
}

func TestExecutorExecuteAllAggregatesErrors(t *testing.T) {
	manager := newTestManager(t, []string{"alpha", "beta", "gamma"})
	manager.kubeconfig = "/does/not/exist"
	delete(manager.clients, "gamma")

	executor := NewExecutor(manager)
	results, err := executor.Execute(context.Background(), "", func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
		if clusterName == "beta" {
			return nil, errors.New("beta failed")
		}
		return clusterName + "-ok", nil
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("result count = %d, want 3", len(results))
	}

	byCluster := make(map[string]ClusterResult, len(results))
	for _, result := range results {
		byCluster[result.Cluster] = result
	}

	if got := byCluster["alpha"]; got.Result != "alpha-ok" || got.Error != "" {
		t.Fatalf("unexpected alpha result: %#v", got)
	}
	if got := byCluster["beta"]; got.Error != "beta failed" {
		t.Fatalf("unexpected beta result: %#v", got)
	}
	if got := byCluster["gamma"]; got.Error == "" {
		t.Fatalf("expected gamma client error, got %#v", got)
	}
}

func newTestManager(t *testing.T, clusters []string) *ClientManager {
	t.Helper()

	contexts := make(map[string]*clientcmdapi.Context, len(clusters))
	clusterConfigs := make(map[string]*clientcmdapi.Cluster, len(clusters))
	clients := make(map[string]*kubernetes.Clientset, len(clusters))
	for _, name := range clusters {
		contexts[name] = &clientcmdapi.Context{Cluster: name}
		clusterConfigs[name] = &clientcmdapi.Cluster{Server: "https://" + name}
		clients[name] = newTestClientset(t)
	}

	return &ClientManager{
		clients: clients,
		rawConfig: clientcmdapi.Config{
			CurrentContext: clusters[0],
			Contexts:       contexts,
			Clusters:       clusterConfigs,
		},
		currentContext: clusters[0],
	}
}

func newTestClientset(t *testing.T) *kubernetes.Clientset {
	t.Helper()

	client, err := kubernetes.NewForConfig(&rest.Config{Host: "http://127.0.0.1"})
	if err != nil {
		t.Fatalf("NewForConfig() error = %v", err)
	}
	return client
}
