package ai

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/kubestellar/klaude/pkg/ai/claude"
	"github.com/kubestellar/klaude/pkg/cluster"
)

type queryOptions struct {
	configFlags   *genericclioptions.ConfigFlags
	query         string
	model         string
	includeStatus bool
}

// NewQueryCommand creates the query command for natural language queries
func NewQueryCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	o := &queryOptions{
		configFlags: configFlags,
	}

	cmd := &cobra.Command{
		Use:   "query <natural language query>",
		Short: "Ask a natural language question about your clusters",
		Long: `Use natural language to ask questions about your Kubernetes clusters,
get troubleshooting help, or request command suggestions.

The AI assistant has context about your available clusters and can help with:
- Explaining Kubernetes resources and concepts
- Troubleshooting issues
- Suggesting kubectl commands
- Analyzing cluster state

Examples:
  # Ask about pods
  kubestellar-ops query "show me all pods that are not running"

  # Get troubleshooting help
  kubestellar-ops query "why might my deployment be failing to start?"

  # Get command suggestions
  kubestellar-ops query "how do I scale my nginx deployment to 5 replicas?"

  # Ask about cluster state
  kubestellar-ops query "what's the overall health of my cluster?"`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			o.query = strings.Join(args, " ")
			return o.run(cmd.Context())
		},
	}

	cmd.Flags().StringVar(&o.model, "model", claude.DefaultModel, "Claude model to use")
	cmd.Flags().BoolVar(&o.includeStatus, "include-status", false, "Include current cluster status in context")

	return cmd
}

func (o *queryOptions) run(ctx context.Context) error {
	// Create Claude client
	client, err := claude.NewClient(claude.WithModel(o.model))
	if err != nil {
		return fmt.Errorf("failed to create Claude client: %w\n\nMake sure ANTHROPIC_API_KEY environment variable is set", err)
	}

	// Get cluster context
	kubeconfig := ""
	if o.configFlags.KubeConfig != nil {
		kubeconfig = *o.configFlags.KubeConfig
	}

	discoverer := cluster.NewDiscoverer(kubeconfig)
	clusters, err := discoverer.DiscoverClusters("all")
	if err != nil {
		fmt.Printf("Warning: failed to discover clusters: %v\n", err)
	}

	// Build cluster context
	clusterCtx := claude.ClusterContext{
		Clusters: make([]string, 0, len(clusters)),
	}

	for _, c := range clusters {
		clusterCtx.Clusters = append(clusterCtx.Clusters, c.Name)
		if c.Current {
			clusterCtx.CurrentCluster = c.Name
		}
	}

	// Get current namespace if set
	if o.configFlags.Namespace != nil && *o.configFlags.Namespace != "" {
		clusterCtx.CurrentNamespace = *o.configFlags.Namespace
	}

	// Build prompts
	systemPrompt := claude.BuildSystemPrompt(clusterCtx)
	userQuery := o.query

	// Optional: include cluster status
	if o.includeStatus && clusterCtx.CurrentCluster != "" {
		// Find current cluster and get basic status
		for _, c := range clusters {
			if c.Current {
				health, err := discoverer.CheckHealth(c)
				if err == nil {
					userQuery = claude.BuildQueryPrompt(o.query, fmt.Sprintf(
						"Current cluster: %s\nStatus: %s\nNodes: %s\nAPI Server: %s",
						c.Name, health.Status, health.NodesReady, health.APIServerStatus,
					))
				}
				break
			}
		}
	}

	// Send query to Claude
	fmt.Println("Thinking...")

	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	response, err := client.Query(queryCtx, systemPrompt, userQuery)
	if err != nil {
		return fmt.Errorf("failed to get response: %w", err)
	}

	// Print response
	fmt.Println()
	fmt.Println(response)

	return nil
}
