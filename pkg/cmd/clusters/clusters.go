package clusters

import (
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

// NewClustersCommand creates the clusters command group
func NewClustersCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clusters",
		Short: "Manage and discover Kubernetes clusters",
		Long: `Commands for discovering, listing, and managing multiple Kubernetes clusters.

Clusters can be discovered from:
  - kubeconfig contexts
  - KubeStellar ManagedCluster resources
  - Open Cluster Management (OCM)

Examples:
  # List all clusters
  klaude-ops clusters list

  # Show cluster health
  klaude-ops clusters health`,
	}

	cmd.AddCommand(newListCommand(configFlags))
	cmd.AddCommand(newHealthCommand(configFlags))

	return cmd
}
