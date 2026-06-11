package server

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/tools/upgrades"
)

// clusterVersionGVR mirrors the unexported GVR in the upgrades package.
// Needed by test helpers that construct fake dynamic clients.
var clusterVersionGVR = schema.GroupVersionResource{
	Group:    "config.openshift.io",
	Version:  "v1",
	Resource: "clusterversions",
}

// toolDetectClusterType bridges the old server method API (used by tests)
// to the new upgrades.DetectClusterType function via serverClusterAccess.
func (s *Server) toolDetectClusterType(ctx context.Context, args map[string]interface{}) (string, bool) {
	return upgrades.DetectClusterType(ctx, &serverClusterAccess{s: s}, args)
}
