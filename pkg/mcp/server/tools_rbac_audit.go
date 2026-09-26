package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/handlers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func toolAuditKubeconfig(ctx context.Context, d *handlers.Deps, args map[string]interface{}) (string, bool) {
	timeoutSeconds := 5
	if v, ok := args["timeout_seconds"].(float64); ok {
		timeoutSeconds = int(v)
	}

	// Load kubeconfig
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if d.Kubeconfig != "" {
		loadingRules.ExplicitPath = d.Kubeconfig
	}

	config, err := loadingRules.Load()
	if err != nil {
		return fmt.Sprintf("Failed to load kubeconfig: %v", err), true
	}

	if len(config.Contexts) == 0 {
		return "No contexts found in kubeconfig", false
	}

	type clusterResult struct {
		Context    string
		Cluster    string
		Server     string
		User       string
		Accessible bool
		Error      string
		IsCurrent  bool
		ServerInfo string
	}

	results := make([]clusterResult, 0, len(config.Contexts))

	for contextName, contextInfo := range config.Contexts {
		result := clusterResult{
			Context:   contextName,
			Cluster:   contextInfo.Cluster,
			User:      contextInfo.AuthInfo,
			IsCurrent: contextName == config.CurrentContext,
		}

		// Get cluster info
		if clusterInfo, ok := config.Clusters[contextInfo.Cluster]; ok {
			result.Server = clusterInfo.Server
		}

		// Try to connect with timeout
		clientConfig := clientcmd.NewDefaultClientConfig(*config, &clientcmd.ConfigOverrides{
			CurrentContext: contextName,
		})

		restConfig, err := clientConfig.ClientConfig()
		if err != nil {
			result.Accessible = false
			result.Error = fmt.Sprintf("Config error: %v", err)
			results = append(results, result)
			continue
		}

		// Set timeout
		restConfig.Timeout = time.Duration(timeoutSeconds) * time.Second

		clientset, err := kubernetes.NewForConfig(restConfig)
		if err != nil {
			result.Accessible = false
			result.Error = fmt.Sprintf("Client error: %v", err)
			results = append(results, result)
			continue
		}

		// Try to get server version (lightweight API call)
		timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
		version, err := clientset.Discovery().ServerVersion()
		cancel()
		_ = timeoutCtx // avoid unused variable

		if err != nil {
			result.Accessible = false
			// Simplify common error messages
			errStr := err.Error()
			if strings.Contains(errStr, "certificate") {
				result.Error = "Certificate error (expired or invalid)"
			} else if strings.Contains(errStr, "connection refused") {
				result.Error = "Connection refused (cluster may be down)"
			} else if strings.Contains(errStr, "no such host") {
				result.Error = "DNS resolution failed (host not found)"
			} else if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline exceeded") {
				result.Error = "Connection timeout"
			} else if strings.Contains(errStr, "unauthorized") || strings.Contains(errStr, "Unauthorized") {
				result.Error = "Unauthorized (credentials may be expired)"
			} else if strings.Contains(errStr, "forbidden") || strings.Contains(errStr, "Forbidden") {
				result.Error = "Forbidden (insufficient permissions)"
			} else {
				result.Error = errStr
			}
		} else {
			result.Accessible = true
			result.ServerInfo = fmt.Sprintf("v%s", version.GitVersion)
		}

		results = append(results, result)
	}

	// Build report
	var sb strings.Builder
	sb.WriteString("# Kubeconfig Cluster Audit\n\n")

	// Summary
	accessible := 0
	inaccessible := 0
	for _, r := range results {
		if r.Accessible {
			accessible++
		} else {
			inaccessible++
		}
	}

	_, _ = fmt.Fprintf(&sb, "**Total contexts:** %d\n", len(results))
	_, _ = fmt.Fprintf(&sb, "**Accessible:** %d\n", accessible)
	_, _ = fmt.Fprintf(&sb, "**Inaccessible:** %d\n\n", inaccessible)

	// Accessible clusters
	if accessible > 0 {
		sb.WriteString("## Accessible Clusters\n\n")
		for _, r := range results {
			if r.Accessible {
				current := ""
				if r.IsCurrent {
					current = " **(current)**"
				}
				_, _ = fmt.Fprintf(&sb, "- **%s**%s\n", r.Context, current)
				_, _ = fmt.Fprintf(&sb, "  - Server: %s\n", r.Server)
				_, _ = fmt.Fprintf(&sb, "  - Version: %s\n", r.ServerInfo)
			}
		}
		sb.WriteString("\n")
	}

	// Inaccessible clusters
	if inaccessible > 0 {
		sb.WriteString("## Inaccessible Clusters\n\n")
		for _, r := range results {
			if !r.Accessible {
				current := ""
				if r.IsCurrent {
					current = " **(current)**"
				}
				_, _ = fmt.Fprintf(&sb, "- **%s**%s\n", r.Context, current)
				_, _ = fmt.Fprintf(&sb, "  - Server: %s\n", r.Server)
				_, _ = fmt.Fprintf(&sb, "  - Error: %s\n", r.Error)
			}
		}
		sb.WriteString("\n")
	}

	// Find duplicate contexts (same server URL)
	serverToContexts := make(map[string][]string)
	for _, r := range results {
		if r.Server != "" {
			serverToContexts[r.Server] = append(serverToContexts[r.Server], r.Context)
		}
	}

	// Check for consolidation opportunities
	hasDuplicates := false
	for _, contexts := range serverToContexts {
		if len(contexts) > 1 {
			hasDuplicates = true
			break
		}
	}

	if hasDuplicates {
		sb.WriteString("## Consolidation Suggestions\n\n")
		sb.WriteString("The following contexts point to the same cluster and could be consolidated:\n\n")
		for server, contexts := range serverToContexts {
			if len(contexts) > 1 {
				_, _ = fmt.Fprintf(&sb, "**Server:** `%s`\n", server)
				sb.WriteString("- Contexts: ")
				for i, ctx := range contexts {
					if i > 0 {
						sb.WriteString(", ")
					}
					_, _ = fmt.Fprintf(&sb, "`%s`", ctx)
				}
				sb.WriteString("\n")
				_, _ = fmt.Fprintf(&sb, "- Consider keeping one and removing %d duplicate(s)\n\n", len(contexts)-1)
			}
		}
	}

	// Cleanup recommendations for inaccessible clusters
	if inaccessible > 0 {
		sb.WriteString("## Delete Inaccessible Contexts\n\n")
		sb.WriteString("These contexts are unreachable and should be removed:\n\n")
		sb.WriteString("```bash\n")
		for _, r := range results {
			if !r.Accessible {
				_, _ = fmt.Fprintf(&sb, "kubectl config delete-context %s\n", r.Context)
			}
		}
		sb.WriteString("```\n\n")

		// Collect unique clusters and users from inaccessible contexts
		clustersToDelete := make(map[string]bool)
		usersToDelete := make(map[string]bool)
		for _, r := range results {
			if !r.Accessible {
				clustersToDelete[r.Cluster] = true
				usersToDelete[r.User] = true
			}
		}

		// Check if clusters/users are used by accessible contexts
		for _, r := range results {
			if r.Accessible {
				delete(clustersToDelete, r.Cluster)
				delete(usersToDelete, r.User)
			}
		}

		if len(clustersToDelete) > 0 || len(usersToDelete) > 0 {
			sb.WriteString("Also remove orphaned clusters and users:\n")
			sb.WriteString("```bash\n")
			for cluster := range clustersToDelete {
				_, _ = fmt.Fprintf(&sb, "kubectl config delete-cluster %s\n", cluster)
			}
			for user := range usersToDelete {
				_, _ = fmt.Fprintf(&sb, "kubectl config delete-user %s\n", user)
			}
			sb.WriteString("```\n")
		}
	}

	// Summary
	if inaccessible == 0 && !hasDuplicates {
		sb.WriteString("## All Good!\n\n")
		sb.WriteString("All clusters are accessible and no duplicates found.\n")
	}

	return sb.String(), false
}

// OPA Gatekeeper Tools

const (
	gatekeeperNamespace          = "gatekeeper-system"
	ownershipTemplateName        = "k8srequiredlabels"
	ownershipConstraintName      = "require-ownership-labels"
	constraintTemplateAPIVersion = "templates.gatekeeper.sh/v1"
	constraintAPIVersion         = "constraints.gatekeeper.sh/v1beta1"
)
