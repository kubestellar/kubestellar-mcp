package clusters

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/kubestellar/kubectl-claude/pkg/cluster"
)

type healthOptions struct {
	configFlags   *genericclioptions.ConfigFlags
	clusterName   string
	allClusters   bool
}

func newHealthCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	o := &healthOptions{
		configFlags: configFlags,
	}

	cmd := &cobra.Command{
		Use:   "health [cluster-name]",
		Short: "Show health status of clusters",
		Long: `Display health information for one or more Kubernetes clusters.

Health checks include:
  - API server connectivity
  - Node status
  - Component status (scheduler, controller-manager, etcd)
  - Resource usage summary

Examples:
  # Check health of all clusters
  kubectl claude clusters health --all-clusters

  # Check health of specific cluster
  kubectl claude clusters health prod-east

  # Check health of current context
  kubectl claude clusters health`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				o.clusterName = args[0]
			}
			return o.run()
		},
	}

	cmd.Flags().BoolVar(&o.allClusters, "all-clusters", false, "Check health of all discovered clusters")

	return cmd
}

func (o *healthOptions) run() error {
	// Get kubeconfig path
	kubeconfig := ""
	if o.configFlags.KubeConfig != nil {
		kubeconfig = *o.configFlags.KubeConfig
	}

	discoverer := cluster.NewDiscoverer(kubeconfig)

	var clustersToCheck []cluster.ClusterInfo
	var err error

	if o.allClusters {
		clustersToCheck, err = discoverer.DiscoverClusters("all")
		if err != nil {
			return fmt.Errorf("failed to discover clusters: %w", err)
		}
	} else if o.clusterName != "" {
		// Find specific cluster
		all, err := discoverer.DiscoverClusters("all")
		if err != nil {
			return fmt.Errorf("failed to discover clusters: %w", err)
		}
		for _, c := range all {
			if c.Name == o.clusterName {
				clustersToCheck = append(clustersToCheck, c)
				break
			}
		}
		if len(clustersToCheck) == 0 {
			return fmt.Errorf("cluster %q not found", o.clusterName)
		}
	} else {
		// Use current context
		clustersToCheck, err = discoverer.DiscoverClusters("kubeconfig")
		if err != nil {
			return fmt.Errorf("failed to discover clusters: %w", err)
		}
		// Filter to only current
		var current []cluster.ClusterInfo
		for _, c := range clustersToCheck {
			if c.Current {
				current = append(current, c)
				break
			}
		}
		clustersToCheck = current
	}

	if len(clustersToCheck) == 0 {
		fmt.Println("No clusters to check")
		return nil
	}

	// Check health of each cluster
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "CLUSTER\tSTATUS\tNODES\tAPI SERVER\tMESSAGE")

	for _, c := range clustersToCheck {
		health, err := discoverer.CheckHealth(c)
		if err != nil {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				c.Name,
				"Error",
				"-",
				"-",
				err.Error(),
			)
			continue
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			c.Name,
			health.Status,
			health.NodesReady,
			health.APIServerStatus,
			health.Message,
		)
	}

	return w.Flush()
}
