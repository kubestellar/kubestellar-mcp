package handlers

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ListEventsForCluster builds a client for cluster, lists events (namespace-
// scoped if namespace is non-empty, otherwise cluster-wide) using the given
// listOpts, and formats a single "error: true" tuple shared by every tool
// handler that starts by listing events. Callers that get a non-empty
// errMsg should return it as-is. This mirrors the listPodsForCluster helper
// pattern in pkg/mcp/server/diagnostics.
func (d *Deps) ListEventsForCluster(ctx context.Context, cluster, namespace string, listOpts metav1.ListOptions) (events *corev1.EventList, errMsg string, isErr bool) {
	client, err := d.GetClientForCluster(cluster)
	if err != nil {
		return nil, fmt.Sprintf("Failed to create client: %v", err), true
	}

	if namespace == "" {
		events, err = client.CoreV1().Events("").List(ctx, listOpts)
	} else {
		events, err = client.CoreV1().Events(namespace).List(ctx, listOpts)
	}
	if err != nil {
		return nil, fmt.Sprintf("Failed to list events: %v", err), true
	}

	return events, "", false
}
