package workloads

import (
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/protocol"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/handlers"
)

// Register adds the workload-domain tools to reg. pkg/mcp/server calls it
// from tools_workloads_registry.go's init(), so the tools' positions in
// tools/list are unchanged from when they were registered directly in that
// file. The workload-diagnostics tools that used to be registered
// immediately after these (find_pod_issues, find_deployment_issues,
// check_resource_limits, check_security_issues, analyze_namespace,
// get_warning_events) are now registered by the sibling
// pkg/mcp/server/diagnostics package's own Register, called right after
// this one, so the overall tools/list order does not change (#1027).
func Register(reg *handlers.Registry) {
	reg.Register(protocol.Tool{
		Name:        "get_pods",
		Description: "List pods in a cluster",
		InputSchema: protocol.InputSchema{
			Type: "object",
			Properties: map[string]protocol.Property{
				"cluster": {
					Type:        "string",
					Description: "Cluster name (uses current context if not specified)",
				},
				"namespace": {
					Type:        "string",
					Description: "Namespace to list pods from (all namespaces if not specified)",
				},
				"label_selector": {
					Type:        "string",
					Description: "Label selector to filter pods (e.g., app=nginx)",
				},
			},
		},
	},
		toolGetPods,
	)
	reg.Register(protocol.Tool{
		Name:        "get_deployments",
		Description: "List deployments in a cluster",
		InputSchema: protocol.InputSchema{
			Type: "object",
			Properties: map[string]protocol.Property{
				"cluster": {
					Type:        "string",
					Description: "Cluster name (uses current context if not specified)",
				},
				"namespace": {
					Type:        "string",
					Description: "Namespace to list deployments from (all namespaces if not specified)",
				},
			},
		},
	},
		toolGetDeployments,
	)
	reg.Register(protocol.Tool{
		Name:        "get_services",
		Description: "List services in a cluster",
		InputSchema: protocol.InputSchema{
			Type: "object",
			Properties: map[string]protocol.Property{
				"cluster": {
					Type:        "string",
					Description: "Cluster name (uses current context if not specified)",
				},
				"namespace": {
					Type:        "string",
					Description: "Namespace to list services from (all namespaces if not specified)",
				},
			},
		},
	},
		toolGetServices,
	)
	reg.Register(protocol.Tool{
		Name:        "get_nodes",
		Description: "List nodes in a cluster",
		InputSchema: protocol.InputSchema{
			Type: "object",
			Properties: map[string]protocol.Property{
				"cluster": {
					Type:        "string",
					Description: "Cluster name (uses current context if not specified)",
				},
			},
		},
	},
		toolGetNodes,
	)
	reg.Register(protocol.Tool{
		Name:        "get_events",
		Description: "Get recent events from a cluster, useful for troubleshooting",
		InputSchema: protocol.InputSchema{
			Type: "object",
			Properties: map[string]protocol.Property{
				"cluster": {
					Type:        "string",
					Description: "Cluster name (uses current context if not specified)",
				},
				"namespace": {
					Type:        "string",
					Description: "Namespace to get events from (all namespaces if not specified)",
				},
				"limit": {
					Type:        "integer",
					Description: "Maximum number of events to return (default 50)",
				},
			},
		},
	},
		toolGetEvents,
	)
	reg.Register(protocol.Tool{
		Name:        "describe_pod",
		Description: "Get detailed information about a specific pod",
		InputSchema: protocol.InputSchema{
			Type: "object",
			Properties: map[string]protocol.Property{
				"cluster": {
					Type:        "string",
					Description: "Cluster name (uses current context if not specified)",
				},
				"namespace": {
					Type:        "string",
					Description: "Namespace of the pod",
				},
				"name": {
					Type:        "string",
					Description: "Name of the pod",
				},
			},
			Required: []string{"name"},
		},
	},
		toolDescribePod,
	)
	reg.Register(protocol.Tool{
		Name:        "get_pod_logs",
		Description: "Get logs from a pod",
		InputSchema: protocol.InputSchema{
			Type: "object",
			Properties: map[string]protocol.Property{
				"cluster": {
					Type:        "string",
					Description: "Cluster name (uses current context if not specified)",
				},
				"namespace": {
					Type:        "string",
					Description: "Namespace of the pod",
				},
				"name": {
					Type:        "string",
					Description: "Name of the pod",
				},
				"container": {
					Type:        "string",
					Description: "Container name (required if pod has multiple containers)",
				},
				"tail_lines": {
					Type:        "integer",
					Description: "Number of lines from the end to return (default 100)",
				},
			},
			Required: []string{"name"},
		},
	},
		toolGetPodLogs,
	)
}
