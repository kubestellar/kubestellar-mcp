package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/klog/v2"

	"github.com/kubestellar/kubestellar-mcp/internal/version/versioncmd"
	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp"
	"github.com/kubestellar/kubestellar-mcp/pkg/metrics"
)

var (
	mcpServer             bool
	metricsAddr           string
	runMCPServer          func(context.Context) error = mcp.RunMCPServer
	newRootCommand                                    = NewRootCommand
	startMetricsServer                                = metrics.StartServer
	shutdownMetricsServer                             = metrics.Shutdown
	signalNotify                                      = signal.Notify
	stderr                io.Writer                   = os.Stderr
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
				klog.InfoS("starting MCP server", "server", "kubestellar-deploy", "metricsEnabled", metricsAddr != "")

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
					klog.InfoS("metrics server listening", "addr", metricsAddr)
					defer func() {
						shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
						defer shutdownCancel()
						_ = shutdownMetricsServer(shutdownCtx, metricsSrv)
					}()
				}

				// Wire SIGINT/SIGTERM into ctx cancellation so this
				// unblocks any in-flight rpcloop.Loop tool handler and lets
				// the deferred metrics shutdown above run cleanly, instead
				// of the process exiting immediately on the signal's
				// default disposition with no cleanup at all. This
				// previously relied solely on cmd.Context() (always
				// context.Background(), since Execute() never calls
				// ExecuteContext), unlike the sibling kubestellar-ops
				// server in pkg/cmd/root.go, whose RunMCPServer doc
				// comment already assumed a cancellable ctx was wired in
				// by its caller (see kubestellar-mcp#1077).
				parentCtx := cmd.Context()
				if parentCtx == nil {
					// cmd.Context() is nil unless Execute() went through
					// cobra's ExecuteContext path; this package's Execute()
					// (and any test calling cmd.RunE directly) never does,
					// so fall back to Background rather than panicking
					// context.WithCancel(nil).
					parentCtx = context.Background()
				}
				ctx, cancel := context.WithCancel(parentCtx)
				defer cancel()

				sigCh := make(chan os.Signal, 1)
				signalNotify(sigCh, syscall.SIGINT, syscall.SIGTERM)

				go func() {
					sig := <-sigCh
					klog.InfoS("received shutdown signal", "signal", sig)
					cancel()
				}()

				err := runMCPServer(ctx)
				if err != nil {
					klog.ErrorS(err, "MCP server stopped with an error")
				} else {
					klog.InfoS("MCP server stopped")
				}
				return err
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
