package server

import "github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/cluster"

// init wires the cluster domain package's tool defs into this server's
// package-level toolRegistry. The domain package (pkg/mcp/server/cluster)
// owns the "list_clusters" and "get_cluster_health" handlers plus their
// schemas; this file only bridges them into the server's dispatch map so
// TestHandleToolsCallDispatch and callTool continue to see the same tool
// names with byte-identical schemas. Extracted from
// tools_cluster{,_registry}.go as part of kubestellar-mcp#1027.
func init() {
	for _, td := range cluster.Tools() {
		toolRegistry.Register(td.Schema, td.Handler)
	}
}
