package mcp

import (
	"context"
	"encoding/json"
	"io"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/deploy"
	"github.com/kubestellar/kubestellar-mcp/pkg/gitops"
)

// This file adapts the pkg/deploy/mcp/deploy sub-package (the "deploy"
// domain: list_cluster_capabilities, find_clusters_for_workload, deploy_app,
// scale_app, patch_app) into the root Server, per epic #983 (decompose
// pkg/deploy/mcp into per-domain sub-packages). It replaces the former
// tools_deploy.go, deploy_apply.go, and deploy_cluster_ops.go, whose logic
// was moved verbatim into pkg/deploy/mcp/deploy. Behavior, including
// tools/list registration order (registry.go:19-24), is unchanged.
//
// Re-exports below exist solely so in-package tests (tools_deploy*_test.go,
// tools_patch_app_test.go, tools_scale_*_test.go, tools_app_cluster_ops_test.go,
// tools_namespace_validation_rest_test.go) keep compiling against *Server
// without modification; they are thin delegations to the deploy sub-package,
// mirroring the pattern used in app_adapter.go and kubectl_adapter.go.

// DeployResult is re-exported from the deploy sub-package.
type DeployResult = deploy.DeployResult

// manifestReaderAdapter adapts *gitops.ManifestReader to deploy.ManifestReader.
type manifestReaderAdapter struct {
	reader *gitops.ManifestReader
}

func (a manifestReaderAdapter) ReadFromReader(r io.Reader) ([]gitops.Manifest, error) {
	return a.reader.ReadFromReader(r)
}

// deployDeps builds a deploy.Deps bound to this *Server, wiring the shared
// multicluster manager/executor/selector and manifest reader/syncer
// factories, plus the manifest-doc validator that stays in the root
// package (manifest_util.go, per epic #983's non-goals).
func (s *Server) deployDeps() deploy.Deps {
	return deploy.Deps{
		Execute:                   s.executor.Execute,
		ExecuteOnSelected:         s.executor.ExecuteOnSelected,
		DiscoverClusters:          s.manager.DiscoverClusters,
		GetConfig:                 s.manager.GetConfig,
		GetCapabilitiesForCluster: s.selector.GetCapabilitiesForCluster,
		GetClusterCapabilities:    s.selector.GetClusterCapabilities,
		FindClustersForWorkload:   s.selector.FindClustersForWorkload,
		GetManifestReader: func() deploy.ManifestReader {
			return manifestReaderAdapter{reader: s.getManifestReader()}
		},
		GetManifestSyncer: func(config *rest.Config) (deploy.ManifestSyncer, error) {
			return s.getManifestSyncer(config)
		},
		ValidateManifestDocs: validateManifestDocs,
	}
}

// deployToolDefs returns the tool definitions handled by the deploy
// sub-package. Order is preserved from the pre-refactor tools_deploy.go so
// tools/list output stays byte-identical (see registry.go:19-24).
func (s *Server) deployToolDefs() []toolDef {
	subDefs := deploy.Tools(s.deployDeps())
	defs := make([]toolDef, 0, len(subDefs))
	for _, d := range subDefs {
		defs = append(defs, toolDef{
			Name:        d.Name,
			Description: d.Description,
			InputSchema: d.InputSchema,
			Handler:     d.Handler,
		})
	}
	return defs
}

// handleListClusterCapabilities delegates to deploy.HandleListClusterCapabilities.
// Retained on *Server for test compatibility.
func (s *Server) handleListClusterCapabilities(ctx context.Context, args json.RawMessage) (interface{}, error) {
	return deploy.HandleListClusterCapabilities(ctx, s.deployDeps(), args)
}

// handleFindClustersForWorkload delegates to deploy.HandleFindClustersForWorkload.
// Retained on *Server for test compatibility.
func (s *Server) handleFindClustersForWorkload(ctx context.Context, args json.RawMessage) (interface{}, error) {
	return deploy.HandleFindClustersForWorkload(ctx, s.deployDeps(), args)
}

// handleDeployApp delegates to deploy.HandleDeployApp. Retained on *Server
// for test compatibility.
func (s *Server) handleDeployApp(ctx context.Context, args json.RawMessage) (interface{}, error) {
	return deploy.HandleDeployApp(ctx, s.deployDeps(), args)
}

// handleScaleApp delegates to deploy.HandleScaleApp. Retained on *Server
// for test compatibility.
func (s *Server) handleScaleApp(ctx context.Context, args json.RawMessage) (interface{}, error) {
	return deploy.HandleScaleApp(ctx, s.deployDeps(), args)
}

// handlePatchApp delegates to deploy.HandlePatchApp. Retained on *Server
// for test compatibility.
func (s *Server) handlePatchApp(ctx context.Context, args json.RawMessage) (interface{}, error) {
	return deploy.HandlePatchApp(ctx, s.deployDeps(), args)
}

// applyManifest delegates to the deploy sub-package's applyManifest via
// handleDeployApp's Deps. Retained on *Server for test compatibility
// (tools_deploy_test.go, tools_deploy_apply_manifest_branches_test.go).
func (s *Server) applyManifest(ctx context.Context, client kubernetes.Interface, clusterName, manifest string, dryRun bool) ([]DeployResult, error) {
	return deploy.ApplyManifest(ctx, s.deployDeps(), client, clusterName, manifest, dryRun)
}

// applyDeployment delegates to deploy.ApplyDeployment. Retained on *Server
// for test compatibility.
func (s *Server) applyDeployment(ctx context.Context, client kubernetes.Interface, rawObj map[string]interface{}, namespace string) (string, error) {
	return deploy.ApplyDeployment(ctx, client, rawObj, namespace)
}

// applyService delegates to deploy.ApplyService. Retained on *Server for
// test compatibility.
func (s *Server) applyService(ctx context.Context, client kubernetes.Interface, rawObj map[string]interface{}, namespace string) (string, error) {
	return deploy.ApplyService(ctx, client, rawObj, namespace)
}

// applyConfigMap delegates to deploy.ApplyConfigMap. Retained on *Server
// for test compatibility.
func (s *Server) applyConfigMap(ctx context.Context, client kubernetes.Interface, rawObj map[string]interface{}, namespace string) (string, error) {
	return deploy.ApplyConfigMap(ctx, client, rawObj, namespace)
}

// applySecret delegates to deploy.ApplySecret. Retained on *Server for
// test compatibility.
func (s *Server) applySecret(ctx context.Context, client kubernetes.Interface, rawObj map[string]interface{}, namespace string) (string, error) {
	return deploy.ApplySecret(ctx, client, rawObj, namespace)
}

// scaleAppInCluster delegates to deploy.ScaleAppInCluster. Retained on
// *Server for test compatibility (tools_app_cluster_ops_test.go).
func (s *Server) scaleAppInCluster(ctx context.Context, client *kubernetes.Clientset, clusterName, appName, namespace string, replicas int32) (interface{}, error) {
	return deploy.ScaleAppInCluster(ctx, client, clusterName, appName, namespace, replicas)
}

// patchAppInCluster delegates to deploy.PatchAppInCluster. Retained on
// *Server for test compatibility (tools_app_cluster_ops_test.go).
func (s *Server) patchAppInCluster(ctx context.Context, client *kubernetes.Clientset, clusterName, appName, namespace string, patch []byte, patchType types.PatchType) (interface{}, error) {
	return deploy.PatchAppInCluster(ctx, client, clusterName, appName, namespace, patch, patchType)
}
