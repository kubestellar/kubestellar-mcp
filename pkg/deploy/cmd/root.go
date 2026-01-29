package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp"
)

var (
	mcpServer bool
)

func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kubestellar-deploy",
		Short: "App-centric multi-cluster deployment and operations",
		Long: `kubestellar-deploy provides app-centric multi-cluster deployment and operations.

Work with your apps, not your clusters. kubestellar-deploy automatically discovers
where your apps are running and aggregates results from all clusters.

Key features:
  - App discovery: Find where your apps run across all clusters
  - Unified logs: Aggregate logs from all clusters
  - Smart placement: Deploy to clusters matching criteria (GPU, memory, labels)
  - Blue/green deployments: Zero-downtime deployments across clusters
  - GitOps: Sync clusters from git, detect drift, reconcile

Examples:
  # Start as MCP server (for Claude Code integration)
  kubestellar-deploy --mcp-server

  # Show version
  kubestellar-deploy version`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if mcpServer {
				return mcp.RunMCPServer()
			}
			return cmd.Help()
		},
	}

	cmd.PersistentFlags().BoolVar(&mcpServer, "mcp-server", false, "Run as MCP server for Claude Code integration")

	cmd.AddCommand(newVersionCommand())

	return cmd
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("kubestellar-deploy version dev")
		},
	}
}

func Execute() error {
	rootCmd := NewRootCommand()
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return err
	}
	return nil
}
