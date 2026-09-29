package diagnostics

import (
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/protocol"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/handlers"
)

// Register adds the diagnostics-domain tools to reg. pkg/mcp/server calls
// it from tools_workloads_registry.go's init(), right after
// workloads.Register, so the tools' positions in tools/list are unchanged
// from when they were registered directly from that file (#1027).
func Register(reg *handlers.Registry) {
	reg.Register(protocol.Tool{
		Name:        "find_pod_issues",
		Description: "Find pods with issues like CrashLoopBackOff, ImagePullBackOff, Pending, OOMKilled, or restarts",
		InputSchema: protocol.InputSchema{
			Type: "object",
			Properties: map[string]protocol.Property{
				"cluster": {
					Type:        "string",
					Description: "Cluster name (uses current context if not specified)",
				},
				"namespace": {
					Type:        "string",
					Description: "Namespace to check (all namespaces if not specified)",
				},
				"include_completed": {
					Type:        "string",
					Description: "Include completed/succeeded pods (true/false, default false)",
				},
			},
		},
	},
		toolFindPodIssues,
	)
	reg.Register(protocol.Tool{
		Name:        "find_deployment_issues",
		Description: "Find deployments with issues like unavailable replicas, stuck rollouts, or misconfigurations",
		InputSchema: protocol.InputSchema{
			Type: "object",
			Properties: map[string]protocol.Property{
				"cluster": {
					Type:        "string",
					Description: "Cluster name (uses current context if not specified)",
				},
				"namespace": {
					Type:        "string",
					Description: "Namespace to check (all namespaces if not specified)",
				},
			},
		},
	},
		toolFindDeploymentIssues,
	)
	reg.Register(protocol.Tool{
		Name:        "check_resource_limits",
		Description: "Find pods/containers without CPU or memory limits/requests configured",
		InputSchema: protocol.InputSchema{
			Type: "object",
			Properties: map[string]protocol.Property{
				"cluster": {
					Type:        "string",
					Description: "Cluster name (uses current context if not specified)",
				},
				"namespace": {
					Type:        "string",
					Description: "Namespace to check (all namespaces if not specified)",
				},
			},
		},
	},
		toolCheckResourceLimits,
	)
	reg.Register(protocol.Tool{
		Name:        "check_security_issues",
		Description: "Find security misconfigurations: privileged containers, running as root, host network/PID, missing security context",
		InputSchema: protocol.InputSchema{
			Type: "object",
			Properties: map[string]protocol.Property{
				"cluster": {
					Type:        "string",
					Description: "Cluster name (uses current context if not specified)",
				},
				"namespace": {
					Type:        "string",
					Description: "Namespace to check (all namespaces if not specified)",
				},
			},
		},
	},
		toolCheckSecurityIssues,
	)
	reg.Register(protocol.Tool{
		Name:        "analyze_namespace",
		Description: "Comprehensive namespace analysis: resource quotas, limit ranges, pod count, issues summary",
		InputSchema: protocol.InputSchema{
			Type: "object",
			Properties: map[string]protocol.Property{
				"cluster": {
					Type:        "string",
					Description: "Cluster name (uses current context if not specified)",
				},
				"namespace": {
					Type:        "string",
					Description: "Namespace to analyze",
				},
			},
			Required: []string{"namespace"},
		},
	},
		toolAnalyzeNamespace,
	)
	reg.Register(protocol.Tool{
		Name:        "get_warning_events",
		Description: "Get only Warning events, filtered by namespace or resource",
		InputSchema: protocol.InputSchema{
			Type: "object",
			Properties: map[string]protocol.Property{
				"cluster": {
					Type:        "string",
					Description: "Cluster name (uses current context if not specified)",
				},
				"namespace": {
					Type:        "string",
					Description: "Namespace to check (all namespaces if not specified)",
				},
				"involved_object": {
					Type:        "string",
					Description: "Filter by involved object name",
				},
				"limit": {
					Type:        "integer",
					Description: "Maximum number of events (default 50)",
				},
			},
		},
	},
		toolGetWarningEvents,
	)
}
