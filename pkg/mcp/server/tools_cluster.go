package server

import (
	"fmt"
	"strings"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func (s *Server) toolListClusters(args map[string]interface{}) (string, bool) {
	source := "all"
	if v, ok := args["source"].(string); ok {
		source = v
	}

	clusters, err := s.discoverer.DiscoverClusters(source)
	if err != nil {
		return fmt.Sprintf("Failed to discover clusters: %v", err), true
	}

	if len(clusters) == 0 {
		return "No clusters found", false
	}

	var sb strings.Builder
	sb.WriteString("Discovered clusters:\n\n")

	for _, c := range clusters {
		current := ""
		if c.Current {
			current = " (current)"
		}
		_, _ = fmt.Fprintf(&sb, "- %s%s\n", c.Name, current)
		_, _ = fmt.Fprintf(&sb, "  Source: %s\n", c.Source)
		_, _ = fmt.Fprintf(&sb, "  Server: %s\n", c.Server)
		if c.Status != "" {
			_, _ = fmt.Fprintf(&sb, "  Status: %s\n", c.Status)
		}
		sb.WriteString("\n")
	}

	return sb.String(), false
}

func (s *Server) toolGetClusterHealth(args map[string]interface{}) (string, bool) {
	clusterName, _ := args["cluster"].(string)

	clusters, err := s.discoverer.DiscoverClusters("all")
	if err != nil {
		return fmt.Sprintf("Failed to discover clusters: %v", err), true
	}

	var targetCluster *struct {
		Name    string
		Context string
		Server  string
		Current bool
	}

	for _, c := range clusters {
		if clusterName == "" && c.Current {
			targetCluster = &struct {
				Name    string
				Context string
				Server  string
				Current bool
			}{c.Name, c.Context, c.Server, c.Current}
			break
		}
		if c.Name == clusterName || c.Context == clusterName {
			targetCluster = &struct {
				Name    string
				Context string
				Server  string
				Current bool
			}{c.Name, c.Context, c.Server, c.Current}
			break
		}
	}

	if targetCluster == nil {
		if clusterName == "" {
			return "No current cluster context set", true
		}
		return fmt.Sprintf("Cluster %q not found", clusterName), true
	}

	// Check health
	health, err := s.discoverer.CheckHealthByContext(targetCluster.Context)
	if err != nil {
		return fmt.Sprintf("Failed to check health: %v", err), true
	}

	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, "Cluster: %s\n", targetCluster.Name)
	_, _ = fmt.Fprintf(&sb, "Status: %s\n", health.Status)
	_, _ = fmt.Fprintf(&sb, "API Server: %s\n", health.APIServerStatus)
	_, _ = fmt.Fprintf(&sb, "Nodes Ready: %s\n", health.NodesReady)
	if health.Error != "" {
		_, _ = fmt.Fprintf(&sb, "Error: %s\n", health.Error)
	}

	return sb.String(), false
}

func (s *Server) getClientForCluster(clusterName string) (kubernetes.Interface, error) {
	if s.clientFactory != nil {
		return s.clientFactory(clusterName)
	}

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if s.kubeconfig != "" {
		loadingRules.ExplicitPath = s.kubeconfig
	}

	configOverrides := &clientcmd.ConfigOverrides{}
	if clusterName != "" {
		configOverrides.CurrentContext = clusterName
	}

	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules, configOverrides).ClientConfig()
	if err != nil {
		return nil, err
	}

	return kubernetes.NewForConfig(config)
}

