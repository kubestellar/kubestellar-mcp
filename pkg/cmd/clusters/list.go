package clusters

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/kubestellar/klaude/pkg/cluster"
)

type listOptions struct {
	configFlags *genericclioptions.ConfigFlags
	source      string
}

func newListCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	o := &listOptions{
		configFlags: configFlags,
	}

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all discovered clusters",
		Long: `List all Kubernetes clusters discovered from kubeconfig contexts
and optionally from KubeStellar ManagedCluster resources.

The output shows:
  - Cluster name
  - Source (kubeconfig, kubestellar)
  - Current context marker
  - Server URL
  - Status (if available)

Examples:
  # List all clusters
  kubectl klaude clusters list

  # List only kubeconfig clusters
  kubectl klaude clusters list --source=kubeconfig

  # List only KubeStellar managed clusters
  kubectl klaude clusters list --source=kubestellar`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return o.run()
		},
	}

	cmd.Flags().StringVar(&o.source, "source", "all", "Discovery source: all, kubeconfig, kubestellar")

	return cmd
}

func (o *listOptions) run() error {
	// Get kubeconfig path
	kubeconfig := ""
	if o.configFlags.KubeConfig != nil {
		kubeconfig = *o.configFlags.KubeConfig
	}

	// Discover clusters
	discoverer := cluster.NewDiscoverer(kubeconfig)
	clusters, err := discoverer.DiscoverClusters(o.source)
	if err != nil {
		return fmt.Errorf("failed to discover clusters: %w", err)
	}

	if len(clusters) == 0 {
		fmt.Println("No clusters found")
		return nil
	}

	// Print results in table format
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "CURRENT\tNAME\tSOURCE\tSERVER\tSTATUS")

	for _, c := range clusters {
		current := ""
		if c.Current {
			current = "*"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			current,
			c.Name,
			c.Source,
			truncateString(c.Server, 50),
			c.Status,
		)
	}

	return w.Flush()
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
