package server

import "context"

func init() {
	RegisterTool(Tool{
			Name:        "get_pods",
			Description: "List pods in a cluster",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
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
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolGetPods(ctx, args)
		},
	)
	RegisterTool(Tool{
			Name:        "get_deployments",
			Description: "List deployments in a cluster",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
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
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolGetDeployments(ctx, args)
		},
	)
	RegisterTool(Tool{
			Name:        "get_services",
			Description: "List services in a cluster",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
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
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolGetServices(ctx, args)
		},
	)
	RegisterTool(Tool{
			Name:        "get_nodes",
			Description: "List nodes in a cluster",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
				},
			},
		},
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolGetNodes(ctx, args)
		},
	)
	RegisterTool(Tool{
			Name:        "get_events",
			Description: "Get recent events from a cluster, useful for troubleshooting",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
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
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolGetEvents(ctx, args)
		},
	)
	RegisterTool(Tool{
			Name:        "describe_pod",
			Description: "Get detailed information about a specific pod",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
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
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolDescribePod(ctx, args)
		},
	)
	RegisterTool(Tool{
			Name:        "get_pod_logs",
			Description: "Get logs from a pod",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
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
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolGetPodLogs(ctx, args)
		},
	)
	RegisterTool(Tool{
			Name:        "find_pod_issues",
			Description: "Find pods with issues like CrashLoopBackOff, ImagePullBackOff, Pending, OOMKilled, or restarts",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
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
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolFindPodIssues(ctx, args)
		},
	)
	RegisterTool(Tool{
			Name:        "find_deployment_issues",
			Description: "Find deployments with issues like unavailable replicas, stuck rollouts, or misconfigurations",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
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
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolFindDeploymentIssues(ctx, args)
		},
	)
	RegisterTool(Tool{
			Name:        "check_resource_limits",
			Description: "Find pods/containers without CPU or memory limits/requests configured",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
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
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolCheckResourceLimits(ctx, args)
		},
	)
	RegisterTool(Tool{
			Name:        "check_security_issues",
			Description: "Find security misconfigurations: privileged containers, running as root, host network/PID, missing security context",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
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
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolCheckSecurityIssues(ctx, args)
		},
	)
	RegisterTool(Tool{
			Name:        "analyze_namespace",
			Description: "Comprehensive namespace analysis: resource quotas, limit ranges, pod count, issues summary",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
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
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolAnalyzeNamespace(ctx, args)
		},
	)
	RegisterTool(Tool{
			Name:        "get_warning_events",
			Description: "Get only Warning events, filtered by namespace or resource",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
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
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolGetWarningEvents(ctx, args)
		},
	)
	RegisterTool(Tool{
			Name:        "audit_kubeconfig",
			Description: "Audit all clusters in kubeconfig: check connectivity, identify stale/inaccessible clusters, and recommend cleanup",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"timeout_seconds": {
						Type:        "integer",
						Description: "Connection timeout in seconds per cluster (default 5)",
					},
				},
			},
		},
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolAuditKubeconfig(ctx, args)
		},
	)
	RegisterTool(Tool{
			Name:        "find_resource_owners",
			Description: "Find who owns/manages resources by checking managedFields, ownership labels, and annotations",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace to check (required)",
					},
					"resource_type": {
						Type:        "string",
						Description: "Resource type to check: pods, deployments, services, all (default: all)",
					},
				},
				Required: []string{"namespace"},
			},
		},
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolFindResourceOwners(ctx, args)
		},
	)
}
