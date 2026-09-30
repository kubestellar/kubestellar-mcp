package server

import (
	"context"

	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/handlers"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/tools/upgrades"
)

// *handlers.Deps satisfies upgrades.ClusterAccess directly, so no adapter
// is needed between the server and the upgrades sub-package.
var _ upgrades.ClusterAccess = (*handlers.Deps)(nil)

// The upgrade domain lives in pkg/mcp/tools/upgrades (schemas, handlers and
// tests); this file is the registration bridge that keeps detect_cluster_type,
// get_cluster_version_info, check_helm_release_upgrades,
// check_olm_operator_upgrades, get_upgrade_status, get_upgrade_prerequisites
// and trigger_openshift_upgrade at the same positions in tools/list as before
// the extraction (kubestellar-mcp#1027). The domain's types and helpers are no
// longer re-exported here: consumers import pkg/mcp/tools/upgrades directly.
func init() {
	for _, td := range upgrades.Tools() {
		td := td // capture loop variable
		RegisterTool(td.Schema,
			func(ctx context.Context, d *handlers.Deps, args map[string]interface{}) (string, bool) {
				return td.Handler(ctx, d, args)
			},
		)
	}
}
