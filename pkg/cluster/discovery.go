package cluster

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// ClusterInfo contains information about a discovered cluster
type ClusterInfo struct {
	Name    string
	Source  string // "kubeconfig" or "kubestellar"
	Server  string
	Context string
	Current bool
	Status  string
}

// HealthInfo contains health information about a cluster
type HealthInfo struct {
	Status          string
	NodesReady      string
	APIServerStatus string
	Message         string
	Error           string
}

// Discoverer handles cluster discovery from multiple sources
type Discoverer struct {
	kubeconfig string
}

// NewDiscoverer creates a new cluster discoverer
func NewDiscoverer(kubeconfig string) *Discoverer {
	return &Discoverer{
		kubeconfig: kubeconfig,
	}
}

// DiscoverClusters discovers clusters from the specified source
func (d *Discoverer) DiscoverClusters(source string) ([]ClusterInfo, error) {
	var clusters []ClusterInfo

	switch source {
	case "kubeconfig", "all":
		kubeconfigClusters, err := d.discoverFromKubeconfig()
		if err != nil {
			return nil, fmt.Errorf("kubeconfig discovery failed: %w", err)
		}
		clusters = append(clusters, kubeconfigClusters...)
	}

	// TODO: Add KubeStellar discovery when source is "kubestellar" or "all"
	// This will query ManagedCluster CRDs from an ITS cluster

	return clusters, nil
}

// discoverFromKubeconfig discovers clusters from kubeconfig contexts
func (d *Discoverer) discoverFromKubeconfig() ([]ClusterInfo, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if d.kubeconfig != "" {
		loadingRules.ExplicitPath = d.kubeconfig
	}

	config, err := loadingRules.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	var clusters []ClusterInfo

	for contextName, ctx := range config.Contexts {
		clusterConfig, ok := config.Clusters[ctx.Cluster]
		if !ok {
			continue
		}

		clusters = append(clusters, ClusterInfo{
			Name:    contextName,
			Source:  "kubeconfig",
			Server:  clusterConfig.Server,
			Context: contextName,
			Current: contextName == config.CurrentContext,
			Status:  "Unknown",
		})
	}

	return clusters, nil
}

// CheckHealth checks the health of a cluster
func (d *Discoverer) CheckHealth(cluster ClusterInfo) (*HealthInfo, error) {
	client, err := d.buildClient(cluster.Context)
	if err != nil {
		return nil, fmt.Errorf("failed to build client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Check API server
	_, err = client.Discovery().ServerVersion()
	if err != nil {
		return &HealthInfo{
			Status:          "Unhealthy",
			APIServerStatus: "Unreachable",
			Message:         err.Error(),
		}, nil
	}

	// Get nodes
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return &HealthInfo{
			Status:          "Degraded",
			APIServerStatus: "Healthy",
			Message:         "Failed to list nodes: " + err.Error(),
		}, nil
	}

	// Count ready nodes
	readyCount := 0
	totalCount := len(nodes.Items)
	for _, node := range nodes.Items {
		for _, condition := range node.Status.Conditions {
			if condition.Type == "Ready" && condition.Status == "True" {
				readyCount++
				break
			}
		}
	}

	status := "Healthy"
	message := "All systems operational"
	if readyCount < totalCount {
		status = "Degraded"
		message = fmt.Sprintf("%d/%d nodes not ready", totalCount-readyCount, totalCount)
	}

	return &HealthInfo{
		Status:          status,
		NodesReady:      fmt.Sprintf("%d/%d", readyCount, totalCount),
		APIServerStatus: "Healthy",
		Message:         message,
	}, nil
}

// buildClient builds a Kubernetes client for the given context
func (d *Discoverer) buildClient(contextName string) (*kubernetes.Clientset, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if d.kubeconfig != "" {
		loadingRules.ExplicitPath = d.kubeconfig
	}

	configOverrides := &clientcmd.ConfigOverrides{
		CurrentContext: contextName,
	}

	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, err
	}

	return kubernetes.NewForConfig(restConfig)
}

// CheckHealthByContext checks cluster health by context name
func (d *Discoverer) CheckHealthByContext(contextName string) (*HealthInfo, error) {
	return d.CheckHealth(ClusterInfo{Context: contextName})
}

// GetCurrentContext returns the current kubeconfig context name
func (d *Discoverer) GetCurrentContext() (string, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if d.kubeconfig != "" {
		loadingRules.ExplicitPath = d.kubeconfig
	}

	config, err := loadingRules.Load()
	if err != nil {
		return "", err
	}

	return config.CurrentContext, nil
}
