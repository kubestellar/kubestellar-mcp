package cluster

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

const healthCheckTimeout = 10 * time.Second

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

// RawClusterEntry is one context resolved by DiscoverRawClusterEntries from a
// raw kubeconfig api.Config.
type RawClusterEntry struct {
	ContextName string
	Server      string
	Current     bool
}

// DiscoverRawClusterEntries walks rawConfig.Contexts and resolves each
// context's referenced Cluster entry. It is the single place that owns this
// walk: pkg/cluster.Discoverer and pkg/multicluster.ClientManager both
// discover clusters from a raw kubeconfig api.Config and previously
// duplicated this loop, with the two copies silently drifting on how a
// context whose Cluster entry is missing (e.g. from partial edits or merged
// kubeconfig fragments) gets handled.
//
// Such a context is always skipped rather than surfaced as an error, since
// one orphaned context should not prevent discovery of the remaining, valid
// ones. onSkipped, if non-nil, is invoked with the context name and the
// missing cluster reference so a caller can log or otherwise surface the
// condition; a caller that passes nil keeps a silent skip.
func DiscoverRawClusterEntries(rawConfig api.Config, onSkipped func(contextName, clusterRef string)) []RawClusterEntry {
	var entries []RawClusterEntry

	for contextName, ctx := range rawConfig.Contexts {
		clusterConfig, ok := rawConfig.Clusters[ctx.Cluster]
		if !ok {
			if onSkipped != nil {
				onSkipped(contextName, ctx.Cluster)
			}
			continue
		}

		entries = append(entries, RawClusterEntry{
			ContextName: contextName,
			Server:      clusterConfig.Server,
			Current:     contextName == rawConfig.CurrentContext,
		})
	}

	return entries
}

// DiscoverClusters discovers clusters from the specified source
func (d *Discoverer) DiscoverClusters(source string) ([]ClusterInfo, error) {
	var clusters []ClusterInfo

	switch source {
	case "kubeconfig":
		kubeconfigClusters, err := d.discoverFromKubeconfig()
		if err != nil {
			return nil, fmt.Errorf("kubeconfig discovery failed: %w", err)
		}
		clusters = append(clusters, kubeconfigClusters...)
	case "kubestellar":
		return nil, fmt.Errorf("kubestellar cluster discovery is not yet implemented")
	case "all":
		kubeconfigClusters, err := d.discoverFromKubeconfig()
		if err != nil {
			return nil, fmt.Errorf("kubeconfig discovery failed: %w", err)
		}
		clusters = append(clusters, kubeconfigClusters...)
	default:
		return nil, fmt.Errorf("unsupported discovery source %q", source)
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

	for _, entry := range DiscoverRawClusterEntries(*config, nil) {
		clusters = append(clusters, ClusterInfo{
			Name:    entry.ContextName,
			Source:  "kubeconfig",
			Server:  entry.Server,
			Context: entry.ContextName,
			Current: entry.Current,
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

	ctx, cancel := context.WithTimeout(context.Background(), healthCheckTimeout)
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

// NewClientConfig returns a non-interactive kubeconfig client config. An empty
// kubeconfig uses the default loading rules; an empty contextName keeps the
// kubeconfig's current context.
func NewClientConfig(kubeconfig, contextName string) clientcmd.ClientConfig {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		loadingRules.ExplicitPath = kubeconfig
	}

	configOverrides := &clientcmd.ConfigOverrides{}
	if contextName != "" {
		configOverrides.CurrentContext = contextName
	}

	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
}

// buildClient builds a Kubernetes client for the given context
func (d *Discoverer) buildClient(contextName string) (*kubernetes.Clientset, error) {
	restConfig, err := NewClientConfig(d.kubeconfig, contextName).ClientConfig()
	if err != nil {
		return nil, err
	}

	restConfig.Timeout = healthCheckTimeout
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
