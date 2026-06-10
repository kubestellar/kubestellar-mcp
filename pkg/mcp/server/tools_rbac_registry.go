package server

import "context"

func init() {
	RegisterTool(Tool{
			Name:        "get_roles",
			Description: "List Roles in a namespace",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace to list roles from (all namespaces if not specified)",
					},
				},
			},
		},
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolGetRoles(ctx, args)
		},
	)
	RegisterTool(Tool{
			Name:        "get_cluster_roles",
			Description: "List ClusterRoles in a cluster",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"include_system": {
						Type:        "string",
						Description: "Include system ClusterRoles (true/false, default false)",
					},
				},
			},
		},
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolGetClusterRoles(ctx, args)
		},
	)
	RegisterTool(Tool{
			Name:        "get_role_bindings",
			Description: "List RoleBindings in a namespace",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace to list role bindings from (all namespaces if not specified)",
					},
				},
			},
		},
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolGetRoleBindings(ctx, args)
		},
	)
	RegisterTool(Tool{
			Name:        "get_cluster_role_bindings",
			Description: "List ClusterRoleBindings in a cluster",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"include_system": {
						Type:        "string",
						Description: "Include system ClusterRoleBindings (true/false, default false)",
					},
				},
			},
		},
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolGetClusterRoleBindings(ctx, args)
		},
	)
	RegisterTool(Tool{
			Name:        "can_i",
			Description: "Check if a subject can perform an action on a resource (similar to kubectl auth can-i)",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"verb": {
						Type:        "string",
						Description: "The action verb (get, list, create, update, delete, watch, etc.)",
					},
					"resource": {
						Type:        "string",
						Description: "The resource type (pods, deployments, secrets, etc.)",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace for the check (empty for cluster-scoped)",
					},
					"subresource": {
						Type:        "string",
						Description: "Subresource (e.g., logs, status)",
					},
					"name": {
						Type:        "string",
						Description: "Specific resource name to check",
					},
				},
				Required: []string{"verb", "resource"},
			},
		},
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolCanI(ctx, args)
		},
	)
	RegisterTool(Tool{
			Name:        "analyze_subject_permissions",
			Description: "Analyze all RBAC permissions for a specific subject (user, group, or service account)",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"subject_kind": {
						Type:        "string",
						Description: "Kind of subject: User, Group, or ServiceAccount",
						Enum:        []string{"User", "Group", "ServiceAccount"},
					},
					"subject_name": {
						Type:        "string",
						Description: "Name of the subject",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace for ServiceAccount subjects",
					},
				},
				Required: []string{"subject_kind", "subject_name"},
			},
		},
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolAnalyzeSubjectPermissions(ctx, args)
		},
	)
	RegisterTool(Tool{
			Name:        "describe_role",
			Description: "Get detailed information about a Role or ClusterRole including all rules",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"name": {
						Type:        "string",
						Description: "Name of the Role or ClusterRole",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace for Role (omit for ClusterRole)",
					},
				},
				Required: []string{"name"},
			},
		},
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolDescribeRole(ctx, args)
		},
	)
}
