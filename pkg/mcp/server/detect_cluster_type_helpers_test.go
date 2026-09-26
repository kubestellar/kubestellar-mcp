package server

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/handlers"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/tools/upgrades"
)

// clusterVersionGVR mirrors the unexported GVR in the upgrades package.
// Needed by test helpers that construct fake dynamic clients.
var clusterVersionGVR = schema.GroupVersionResource{
	Group:    "config.openshift.io",
	Version:  "v1",
	Resource: "clusterversions",
}

// toolDetectClusterType bridges the server-package test API to the
// upgrades.DetectClusterType function; *handlers.Deps is its ClusterAccess.
func toolDetectClusterType(ctx context.Context, d *handlers.Deps, args map[string]interface{}) (string, bool) {
	return upgrades.DetectClusterType(ctx, d, args)
}
