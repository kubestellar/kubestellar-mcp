package deploy

import (
	"context"
	"encoding/json"
	"io"

	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/tooldef"
	"github.com/kubestellar/kubestellar-mcp/pkg/gitops"
	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// ManifestReader is the narrow slice of *gitops.ManifestReader that
// applyManifest needs.
type ManifestReader interface {
	ReadFromReader(reader io.Reader) ([]gitops.Manifest, error)
}

// ManifestSyncer is the narrow slice of the root package's manifestSyncer
// interface that applyManifest needs.
type ManifestSyncer interface {
	Sync(ctx context.Context, manifests []gitops.Manifest, clusterName string, opts gitops.SyncOptions) (*gitops.SyncSummary, error)
}

// Deps is the narrow set of dependencies the deploy-domain handlers need
// from *Server: the multicluster executor/manager/selector plus the
// manifest reader/syncer factories and the manifest-doc validator that
// stays in the root package (manifest_util.go, per epic #983's non-goals).
type Deps struct {
	Execute                   func(ctx context.Context, clusterName string, fn multicluster.ExecuteFunc) ([]multicluster.ClusterResult, error)
	ExecuteOnSelected         func(ctx context.Context, clusterNames []string, fn multicluster.ExecuteFunc) ([]multicluster.ClusterResult, error)
	DiscoverClusters          func() ([]multicluster.ClusterInfo, error)
	GetConfig                 func(clusterName string) (*rest.Config, error)
	GetCapabilitiesForCluster func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (*multicluster.ClusterCapabilities, error)
	GetClusterCapabilities    func(ctx context.Context) ([]multicluster.ClusterCapabilities, error)
	FindClustersForWorkload   func(ctx context.Context, req multicluster.WorkloadRequirements) ([]string, error)
	GetManifestReader         func() ManifestReader
	GetManifestSyncer         func(config *rest.Config) (ManifestSyncer, error)
	ValidateManifestDocs      func(manifest string) error
}

// Tools returns the deploy-domain tool definitions, bound to the given
// Deps. The order matches the pre-refactor deployToolDefs() in
// pkg/deploy/mcp/tools_deploy.go exactly, since tools/list order must stay
// byte-identical (see registry.go).
func Tools(d Deps) []tooldef.ToolDef {
	return []tooldef.ToolDef{
		{
			Name:        "list_cluster_capabilities",
			Description: "List what each cluster can run: GPU availability, CPU/memory capacity, node labels. Use this to understand cluster resources.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"cluster": map[string]interface{}{
						"type":        "string",
						"description": "Specific cluster (all clusters if not specified)",
					},
				},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
				return HandleListClusterCapabilities(ctx, d, args)
			},
		},
		{
			Name:        "find_clusters_for_workload",
			Description: "Find clusters that can run a workload with specific requirements (GPU, memory, CPU, labels).",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"gpu_type": map[string]interface{}{
						"type":        "string",
						"description": "GPU type required (e.g., nvidia.com/gpu)",
					},
					"min_gpu": map[string]interface{}{
						"type":        "integer",
						"description": "Minimum number of GPUs required",
					},
					"min_memory": map[string]interface{}{
						"type":        "string",
						"description": "Minimum memory required (e.g., 16Gi)",
					},
					"min_cpu": map[string]interface{}{
						"type":        "string",
						"description": "Minimum CPU required (e.g., 4)",
					},
					"labels": map[string]interface{}{
						"type":        "object",
						"description": "Required node labels",
					},
				},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
				return HandleFindClustersForWorkload(ctx, d, args)
			},
		},
		{
			Name:        "deploy_app",
			Description: "Deploy an app to clusters. Can specify clusters explicitly or let kubestellar find matching clusters based on requirements.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"manifest": map[string]interface{}{
						"type":        "string",
						"description": "Kubernetes manifest (YAML)",
					},
					"clusters": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Target clusters (all matching clusters if not specified)",
					},
					"gpu_type": map[string]interface{}{
						"type":        "string",
						"description": "Deploy to clusters with this GPU type",
					},
					"min_gpu": map[string]interface{}{
						"type":        "integer",
						"description": "Deploy to clusters with at least this many GPUs",
					},
					"dry_run": map[string]interface{}{
						"type":        "boolean",
						"description": "Preview changes without applying",
					},
				},
				"required": []string{"manifest"},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
				return HandleDeployApp(ctx, d, args)
			},
		},
		{
			Name:        "scale_app",
			Description: "Scale an app across clusters. Can target specific clusters or all clusters where app runs.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"app": map[string]interface{}{
						"type":        "string",
						"description": "App name",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Namespace",
					},
					"replicas": map[string]interface{}{
						"type":        "integer",
						"description": "Target replica count",
					},
					"clusters": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Target clusters (all clusters where app runs if not specified)",
					},
				},
				"required": []string{"app", "replicas"},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
				return HandleScaleApp(ctx, d, args)
			},
		},
		{
			Name:        "patch_app",
			Description: "Apply a patch to an app across clusters.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"app": map[string]interface{}{
						"type":        "string",
						"description": "App name",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Namespace",
					},
					"patch": map[string]interface{}{
						"type":        "string",
						"description": "JSON or strategic merge patch",
					},
					"patch_type": map[string]interface{}{
						"type":        "string",
						"description": "Patch type: strategic, merge, or json (default: strategic)",
					},
					"clusters": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Target clusters",
					},
				},
				"required": []string{"app", "patch"},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
				return HandlePatchApp(ctx, d, args)
			},
		},
	}
}
