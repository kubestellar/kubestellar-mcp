package handlers

import (
	"fmt"

	"k8s.io/client-go/kubernetes"
)

// ResolveClusterClient extracts the "cluster" argument, extracts and
// validates the "namespace" argument, and resolves a client for that
// cluster — the same three steps every tool handler that lists a single
// namespaced/cluster-wide resource type starts with. Callers that get a
// non-empty errMsg should return it as-is. This mirrors the
// ListEventsForCluster helper pattern for the same reason: the call sites
// are leaf domain packages that intentionally do not import each other.
func (d *Deps) ResolveClusterClient(args map[string]interface{}) (client kubernetes.Interface, cluster, namespace, errMsg string, isErr bool) {
	cluster, _ = args["cluster"].(string)
	namespace, err := ExtractAndValidateNamespace(args)
	if err != nil {
		return nil, cluster, namespace, fmt.Sprintf("error: %v", err), true
	}

	client, err = d.GetClientForCluster(cluster)
	if err != nil {
		return nil, cluster, namespace, fmt.Sprintf("Failed to create client: %v", err), true
	}

	return client, cluster, namespace, "", false
}
