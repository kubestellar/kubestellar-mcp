package clusters

import "github.com/kubestellar/kubestellar-mcp/pkg/cluster"

type clusterDiscoverer interface {
	DiscoverClusters(source string) ([]cluster.ClusterInfo, error)
	CheckHealth(cluster cluster.ClusterInfo) (*cluster.HealthInfo, error)
}

var newDiscoverer = func(kubeconfig string) clusterDiscoverer {
	return cluster.NewDiscoverer(kubeconfig)
}
