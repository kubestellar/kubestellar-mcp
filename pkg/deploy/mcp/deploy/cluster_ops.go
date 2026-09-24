package deploy

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/app"
)

// scaleAppInCluster scales an app in a single cluster
func ScaleAppInCluster(ctx context.Context, client *kubernetes.Clientset, clusterName, appName, namespace string, replicas int32) (interface{}, error) {
	ns := namespace
	if ns == "" {
		ns = "default"
	}

	// Find deployment
	deployments, err := client.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	for _, d := range deployments.Items {
		if app.MatchesApp(d.Name, d.Labels, appName) {
			oldReplicas := int32(1)
			if d.Spec.Replicas != nil {
				oldReplicas = *d.Spec.Replicas
			}
			d.Spec.Replicas = &replicas
			_, err := client.AppsV1().Deployments(ns).Update(ctx, &d, metav1.UpdateOptions{})
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{
				"cluster":     clusterName,
				"deployment":  d.Name,
				"oldReplicas": oldReplicas,
				"newReplicas": replicas,
			}, nil
		}
	}

	return nil, fmt.Errorf("deployment %s not found in cluster %s", appName, clusterName)
}

// patchAppInCluster patches an app in a single cluster
func PatchAppInCluster(ctx context.Context, client *kubernetes.Clientset, clusterName, appName, namespace string, patch []byte, patchType types.PatchType) (interface{}, error) {
	ns := namespace
	if ns == "" {
		ns = "default"
	}

	// Find deployment
	deployments, err := client.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	for _, d := range deployments.Items {
		if app.MatchesApp(d.Name, d.Labels, appName) {
			_, err := client.AppsV1().Deployments(ns).Patch(ctx, d.Name, patchType, patch, metav1.PatchOptions{})
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{
				"cluster":    clusterName,
				"deployment": d.Name,
				"status":     "patched",
			}, nil
		}
	}

	return nil, fmt.Errorf("deployment %s not found in cluster %s", appName, clusterName)
}
