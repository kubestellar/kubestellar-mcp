// Package cluster implements the "list_clusters" and "get_cluster_health"
// tool handlers for the kubestellar-ops MCP server. It was extracted from
// pkg/mcp/server/tools_cluster*.go as part of the flat-package decomposition
// tracked in kubestellar-mcp#1027, mirroring the pkg/deploy/mcp/{kubectl,
// helm, kustomize, ...} extractions in #983. It has no import path back to
// pkg/mcp/server: handlers depend only on pkg/mcp/server/handlers.Deps, and
// the server-side wiring is done via Tools() (see register.go).
package cluster

import (
	"context"
	"fmt"
	"strings"

	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/handlers"
)

// ListClusters implements the "list_clusters" MCP tool. It reports every
// cluster the injected Discoverer knows about, tagged with its source and
// current-context marker.
func ListClusters(_ context.Context, d *handlers.Deps, args map[string]interface{}) (string, bool) {
	source := "all"
	if v, ok := args["source"].(string); ok {
		source = v
	}

	clusters, err := d.Discoverer.DiscoverClusters(source)
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

// GetClusterHealth implements the "get_cluster_health" MCP tool. When the
// caller omits "cluster", it reports on the current-context cluster; when
// they name one, that name is matched against both Name and Context.
func GetClusterHealth(_ context.Context, d *handlers.Deps, args map[string]interface{}) (string, bool) {
	clusterName, _ := args["cluster"].(string)

	clusters, err := d.Discoverer.DiscoverClusters("all")
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

	health, err := d.Discoverer.CheckHealthByContext(targetCluster.Context)
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
