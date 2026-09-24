// Package helm implements the helm_install/helm_uninstall/helm_list/
// helm_rollback MCP tools for the kubestellar-deploy server. It was
// extracted from the flat pkg/deploy/mcp package (epic #983, phase 1) so
// this domain can be built and tested in isolation. See ClusterAccess for
// the narrow surface this package needs from *mcp.Server, mirroring the
// Server/ClusterAccess shape used by pkg/deploy/mcp/gitops.
package helm

import (
	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
)

// ClusterAccess is the narrow slice of *multicluster.ClientManager that the
// helm handlers need: cluster discovery, used as the fallback target set
// when the caller does not specify `clusters`.
type ClusterAccess interface {
	DiscoverClusters() ([]multicluster.ClusterInfo, error)
}

// Server holds the dependencies the helm tool handlers need. It is built by
// the root package's thin adapter (helm_adapter.go), which wires Access to
// *deploy/mcp.Server's multicluster manager.
type Server struct {
	Access ClusterAccess
}

// HelmReleaseInfo represents information about a Helm release
type HelmReleaseInfo struct {
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
	Revision   string `json:"revision"`
	Status     string `json:"status"`
	Chart      string `json:"chart"`
	AppVersion string `json:"app_version"`
}

// HelmResult represents the result of a Helm operation
type HelmResult struct {
	Cluster     string `json:"cluster"`
	ReleaseName string `json:"release_name"`
	Namespace   string `json:"namespace"`
	Status      string `json:"status"`
	Message     string `json:"message,omitempty"`
}
