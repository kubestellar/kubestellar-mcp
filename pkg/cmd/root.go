package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/kubestellar/kubectl-claude/internal/version"
	"github.com/kubestellar/kubectl-claude/pkg/cmd/clusters"
)

var (
	// Global flags
	kubeconfig    string
	allClusters   bool
	targetCluster string

	// Kubernetes config flags
	configFlags *genericclioptions.ConfigFlags
)

// rootCmd represents the base command
var rootCmd = &cobra.Command{
	Use:   "kubectl-claude",
	Short: "AI-powered kubectl plugin for multi-cluster Kubernetes management",
	Long: `kubectl-claude is an AI-powered kubectl plugin that helps you manage
clusters and deployments across multiple Kubernetes clusters.

It provides intelligent assistance for:
  - Multi-cluster discovery and management
  - Deployment operations across clusters
  - Natural language queries about your clusters
  - AI-powered troubleshooting and recommendations

Examples:
  # List all available clusters
  kubectl claude clusters list

  # Get pods across all clusters
  kubectl claude get pods --all-clusters

  # Ask a natural language question
  kubectl claude "show me all failing pods"

  # Deploy to multiple clusters
  kubectl claude deploy nginx:1.25 --clusters=prod-east,prod-west`,
	Version: version.Version,
}

// Execute runs the root command
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	// Add persistent flags from cli-runtime (includes kubeconfig, context, namespace, etc.)
	configFlags = genericclioptions.NewConfigFlags(true)
	configFlags.AddFlags(rootCmd.PersistentFlags())

	// Add our custom flags (don't redefine kubeconfig - it's in configFlags)
	rootCmd.PersistentFlags().BoolVar(&allClusters, "all-clusters", false, "Operate on all discovered clusters")
	rootCmd.PersistentFlags().StringVar(&targetCluster, "target-cluster", "", "Target specific cluster by name")

	// Add subcommands
	rootCmd.AddCommand(clusters.NewClustersCommand(configFlags))
	rootCmd.AddCommand(newVersionCommand())
}

func initConfig() {
	// Set kubeconfig from flag or environment
	if kubeconfig == "" {
		kubeconfig = os.Getenv("KUBECONFIG")
	}
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("kubectl-claude version %s\n", version.Version)
			fmt.Printf("  Build date: %s\n", version.BuildDate)
			fmt.Printf("  Git commit: %s\n", version.GitCommit)
		},
	}
}
