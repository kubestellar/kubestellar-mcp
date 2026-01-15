package multicluster

import (
	"fmt"
	"sync"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

// ClusterInfo represents a discovered cluster
type ClusterInfo struct {
	Name       string            // Context name
	Server     string            // API server URL
	Current    bool              // Is this the current context?
	Labels     map[string]string // Cluster labels (from kubeconfig or annotations)
}

// ClientManager manages Kubernetes clients for multiple clusters
type ClientManager struct {
	kubeconfig     string
	clients        map[string]*kubernetes.Clientset
	configs        map[string]*rest.Config
	mu             sync.RWMutex
	rawConfig      api.Config
	currentContext string
}

// NewClientManager creates a new multi-cluster client manager
func NewClientManager(kubeconfig string) (*ClientManager, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		loadingRules.ExplicitPath = kubeconfig
	}

	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	rawConfig, err := kubeConfig.RawConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	return &ClientManager{
		kubeconfig:     kubeconfig,
		clients:        make(map[string]*kubernetes.Clientset),
		configs:        make(map[string]*rest.Config),
		rawConfig:      rawConfig,
		currentContext: rawConfig.CurrentContext,
	}, nil
}

// DiscoverClusters returns all clusters from kubeconfig
func (m *ClientManager) DiscoverClusters() ([]ClusterInfo, error) {
	var clusters []ClusterInfo

	for contextName, context := range m.rawConfig.Contexts {
		cluster, exists := m.rawConfig.Clusters[context.Cluster]
		if !exists {
			continue
		}

		clusters = append(clusters, ClusterInfo{
			Name:    contextName,
			Server:  cluster.Server,
			Current: contextName == m.currentContext,
			Labels:  make(map[string]string),
		})
	}

	return clusters, nil
}

// GetClient returns a Kubernetes client for the specified cluster
func (m *ClientManager) GetClient(clusterName string) (*kubernetes.Clientset, error) {
	m.mu.RLock()
	client, exists := m.clients[clusterName]
	m.mu.RUnlock()

	if exists {
		return client, nil
	}

	// Create new client
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if client, exists := m.clients[clusterName]; exists {
		return client, nil
	}

	config, err := m.getConfigForContext(clusterName)
	if err != nil {
		return nil, err
	}

	client, err = kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for %s: %w", clusterName, err)
	}

	m.clients[clusterName] = client
	m.configs[clusterName] = config

	return client, nil
}

// GetConfig returns the REST config for the specified cluster
func (m *ClientManager) GetConfig(clusterName string) (*rest.Config, error) {
	m.mu.RLock()
	config, exists := m.configs[clusterName]
	m.mu.RUnlock()

	if exists {
		return config, nil
	}

	// Ensure client is created (which also caches config)
	_, err := m.GetClient(clusterName)
	if err != nil {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.configs[clusterName], nil
}

// getConfigForContext creates a REST config for a specific context
func (m *ClientManager) getConfigForContext(contextName string) (*rest.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if m.kubeconfig != "" {
		loadingRules.ExplicitPath = m.kubeconfig
	}

	configOverrides := &clientcmd.ConfigOverrides{
		CurrentContext: contextName,
	}

	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
	config, err := kubeConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get config for context %s: %w", contextName, err)
	}

	return config, nil
}

// CurrentContext returns the current context name
func (m *ClientManager) CurrentContext() string {
	return m.currentContext
}
