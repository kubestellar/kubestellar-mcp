// Package upgrade provides CLI commands for watching upgrade progress
package upgrade

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/kubestellar/kubestellar-mcp/pkg/progress"
)

var (
	clusterVersionGVR = schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1",
		Resource: "clusterversions",
	}
	clusterOperatorGVR = schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1",
		Resource: "clusteroperators",
	}
	machineConfigPoolGVR = schema.GroupVersionResource{
		Group:    "machineconfiguration.openshift.io",
		Version:  "v1",
		Resource: "machineconfigpools",
	}
)

// NewWatchCommand creates the watch-upgrade command
func NewWatchCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var interval time.Duration

	cmd := &cobra.Command{
		Use:   "watch-upgrade",
		Short: "Watch OpenShift cluster upgrade progress with live progress bar",
		Long: `Watch an OpenShift cluster upgrade in progress with a live-updating progress bar.

The progress bar shows:
  - Overall upgrade percentage
  - ClusterOperator completion status
  - MachineConfigPool update status
  - Estimated time remaining

Examples:
  # Watch upgrade on current context
  kubestellar-ops watch-upgrade

  # Watch upgrade on specific cluster
  kubestellar-ops watch-upgrade --context=prod-cluster

  # Custom refresh interval
  kubestellar-ops watch-upgrade --interval=5s`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return watchUpgrade(cmd.Context(), configFlags, interval)
		},
	}

	cmd.Flags().DurationVar(&interval, "interval", 3*time.Second, "Refresh interval for progress updates")

	return cmd
}

func watchUpgrade(ctx context.Context, configFlags *genericclioptions.ConfigFlags, interval time.Duration) error {
	// Build client config
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if configFlags.KubeConfig != nil && *configFlags.KubeConfig != "" {
		loadingRules.ExplicitPath = *configFlags.KubeConfig
	}

	configOverrides := &clientcmd.ConfigOverrides{}
	if configFlags.Context != nil && *configFlags.Context != "" {
		configOverrides.CurrentContext = *configFlags.Context
	}

	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
	config, err := kubeConfig.ClientConfig()
	if err != nil {
		return fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	// Create dynamic client for OpenShift CRDs
	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}

	// Check if this is an OpenShift cluster
	_, err = dynClient.Resource(clusterVersionGVR).Get(ctx, "version", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("not an OpenShift cluster or ClusterVersion not accessible: %w", err)
	}

	fmt.Println("Watching OpenShift cluster upgrade progress...")
	fmt.Println("Press Ctrl+C to stop")
	fmt.Println()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	bar := progress.NewLiveBar(os.Stdout)
	lastPct := -1

	for {
		select {
		case <-ctx.Done():
			fmt.Println("\nStopped watching.")
			return nil
		default:
			status, err := getUpgradeStatus(ctx, dynClient)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\nError fetching status: %v\n", err)
			} else {
				// Only render if percentage changed
				if status.Percent != lastPct || status.Complete {
					bar.Render(status)
					lastPct = status.Percent
					if status.Complete {
						return nil
					}
				}
			}
			<-ticker.C
		}
	}
}

func getUpgradeStatus(ctx context.Context, dynClient dynamic.Interface) (progress.Status, error) {
	// Get ClusterVersion
	cv, err := dynClient.Resource(clusterVersionGVR).Get(ctx, "version", metav1.GetOptions{})
	if err != nil {
		return progress.Status{}, fmt.Errorf("failed to get ClusterVersion: %w", err)
	}

	// Get desired version
	desiredVersion := getNestedString(cv, "status", "desired", "version")

	// Get the Progressing condition message - this contains all the progress info
	conditions, _, _ := unstructured.NestedSlice(cv.Object, "status", "conditions")
	progressMsg := ""
	for _, cond := range conditions {
		if c, ok := cond.(map[string]interface{}); ok {
			condType, _, _ := unstructured.NestedString(c, "type")
			if condType == "Progressing" {
				progressMsg, _, _ = unstructured.NestedString(c, "message")
				break
			}
		}
	}

	// Check if complete
	if strings.Contains(progressMsg, fmt.Sprintf("Cluster version is %s", desiredVersion)) {
		return progress.Status{
			Label:    desiredVersion,
			Percent:  100,
			Complete: true,
		}, nil
	}

	// Parse progress from message like: "Working towards 4.18.30: 168 of 906 done (18% complete), waiting on cloud-controller-manager"
	pct, done, total, waiting := parseProgressMessage(progressMsg)

	return progress.Status{
		Label:   desiredVersion,
		Percent: pct,
		Done:    done,
		Total:   total,
		Current: waiting,
	}, nil
}

// parseProgressMessage extracts progress info from ClusterVersion Progressing condition message
// Example: "Working towards 4.18.30: 168 of 906 done (18% complete), waiting on cloud-controller-manager"
func parseProgressMessage(msg string) (pct int, done int, total int, waiting string) {
	// Extract percentage: "(\d+)% complete"
	if idx := strings.Index(msg, "% complete"); idx > 0 {
		// Find the start of the number
		start := idx - 1
		for start > 0 && msg[start-1] >= '0' && msg[start-1] <= '9' {
			start--
		}
		fmt.Sscanf(msg[start:idx], "%d", &pct)
	}

	// Extract done/total: "(\d+) of (\d+) done"
	if idx := strings.Index(msg, " of "); idx > 0 {
		// Find done number (before " of ")
		start := idx - 1
		for start > 0 && msg[start-1] >= '0' && msg[start-1] <= '9' {
			start--
		}
		fmt.Sscanf(msg[start:idx], "%d", &done)

		// Find total number (after " of ")
		endIdx := strings.Index(msg[idx+4:], " ")
		if endIdx > 0 {
			fmt.Sscanf(msg[idx+4:idx+4+endIdx], "%d", &total)
		}
	}

	// Extract waiting component: "waiting on (.*)"
	if idx := strings.Index(msg, "waiting on "); idx > 0 {
		waiting = msg[idx+11:]
		// Trim any trailing info
		if commaIdx := strings.Index(waiting, ","); commaIdx > 0 {
			waiting = waiting[:commaIdx]
		}
	}

	return
}

func getNestedString(obj *unstructured.Unstructured, fields ...string) string {
	val, _, _ := unstructured.NestedString(obj.Object, fields...)
	return val
}
