package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/kubestellar/kubestellar-mcp/internal/version/versioncmd"
	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp"
	"github.com/kubestellar/kubestellar-mcp/pkg/metrics"
)

var (
	mcpServer             bool
	metricsAddr           string
	runMCPServer          func(context.Context) error = mcp.RunMCPServer
	newRootCommand                  = NewRootCommand
	startMetricsServer              = metrics.StartServer
	shutdownMetricsServer           = metrics.Shutdown
	stderr                io.Writer = os.Stderr
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
				// Only start the /metrics endpoint when an operator
				// explicitly configures an address; otherwise no HTTP
				// listener is opened and no metrics data is exposed
				// outside the process (mirrors pkg/cmd/root.go's
				// kubestellar-ops wiring). Metrics recorded here land in
				// the same mcpserver_* series as kubestellar-ops with no
				// binary-distinguishing label - an operator enabling this
				// on both binaries must scrape them as distinct
				// Prometheus targets (e.g. separate job/instance labels),
				// see docs/slo.md and docs/alerts/README.md.
				if metricsAddr != "" {
					metricsSrv, err := startMetricsServer(metricsAddr)
					if err != nil {
						_, _ = fmt.Fprintf(stderr, "metrics server error: %v\n", err)
						return err
					}
					defer func() {
						shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
						defer shutdownCancel()
						_ = shutdownMetricsServer(shutdownCtx, metricsSrv)
					}()
				}
				return runMCPServer(cmd.Context())
			}
			return cmd.Help()
		},
	}

	cmd.PersistentFlags().BoolVar(&mcpServer, "mcp-server", false, "Run as MCP server for Claude Code integration")
	cmd.PersistentFlags().StringVar(&metricsAddr, "metrics-addr", "", "Address to serve Prometheus /metrics on (e.g. 127.0.0.1:9091); disabled unless explicitly set")

	cmd.AddCommand(newVersionCommand())

	return cmd
}

func newVersionCommand() *cobra.Command {
	return versioncmd.New("kubestellar-deploy")
}

func Execute() error {
	rootCmd := newRootCommand()
	if err := rootCmd.Execute(); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return err
	}
	return nil
}
