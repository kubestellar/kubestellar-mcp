package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/kubestellar/kubectl-claude/internal/version"
	"github.com/kubestellar/kubectl-claude/pkg/cmd/ai"
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

  # Ask a natural language question (shorthand)
  kubectl claude "show me all failing pods"

  # Ask using query subcommand
  kubectl claude query "why is my pod crashing?"

  # Check cluster health
  kubectl claude clusters health --all-clusters`,
	Version: version.Version,
	// Handle natural language queries directly
	Args: func(cmd *cobra.Command, args []string) error {
		// Allow any args - we'll handle natural language queries in Run
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) > 0 && isNaturalLanguageQuery(args) {
			// Treat as natural language query - run the query subcommand
			queryCmd := ai.NewQueryCommand(configFlags)
			queryCmd.SetArgs(args)
			if err := queryCmd.Execute(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		}
		// Otherwise show help
		cmd.Help()
	},
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
	rootCmd.AddCommand(ai.NewQueryCommand(configFlags))
	rootCmd.AddCommand(newVersionCommand())
}

// isNaturalLanguageQuery checks if args look like a natural language query
// rather than a subcommand
func isNaturalLanguageQuery(args []string) bool {
	if len(args) == 0 {
		return false
	}

	// Known subcommands
	subcommands := map[string]bool{
		"clusters":   true,
		"query":      true,
		"version":    true,
		"help":       true,
		"completion": true,
	}

	first := strings.ToLower(args[0])

	// If it's a known subcommand, it's not a natural language query
	if subcommands[first] {
		return false
	}

	// If it starts with a flag, it's not a natural language query
	if strings.HasPrefix(first, "-") {
		return false
	}

	// Otherwise, treat it as a natural language query
	return true
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
