package clusters

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
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
		Long: `List all Kubernetes clusters discovered from kubeconfig contexts.

KubeStellar ManagedCluster discovery is not yet implemented. Using
--source=kubestellar returns an explicit error until that support lands.

The output shows:
  - Cluster name
  - Source (kubeconfig, kubestellar)
  - Current context marker
  - Server URL
  - Status (if available)

Examples:
  # List all clusters
  kubestellar-ops clusters list

  # List only kubeconfig clusters
  kubestellar-ops clusters list --source=kubeconfig

  # Check whether KubeStellar discovery is available yet
  kubestellar-ops clusters list --source=kubestellar`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return o.run()
		},
	}

	cmd.Flags().StringVar(&o.source, "source", "all", "Discovery source: all, kubeconfig, kubestellar (not yet implemented)")

	return cmd
}

func (o *listOptions) run() error {
	// Get kubeconfig path
	kubeconfig := ""
	if o.configFlags.KubeConfig != nil {
		kubeconfig = *o.configFlags.KubeConfig
	}

	// Discover clusters
	discoverer := newDiscoverer(kubeconfig)
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
	if _, err := fmt.Fprintln(w, "CURRENT\tNAME\tSOURCE\tSERVER\tSTATUS"); err != nil {
		return err
	}

	for _, c := range clusters {
		current := ""
		if c.Current {
			current = "*"
		}
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			current,
			c.Name,
			c.Source,
			truncateString(c.Server, 50),
			c.Status,
		); err != nil {
			return err
		}
	}

	return w.Flush()
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
