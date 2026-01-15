package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// AppInstance represents an app instance in a cluster
type AppInstance struct {
	Cluster      string `json:"cluster"`
	Namespace    string `json:"namespace"`
	Name         string `json:"name"`
	Kind         string `json:"kind"` // Deployment, StatefulSet, DaemonSet
	Replicas     int32  `json:"replicas"`
	ReadyReplicas int32 `json:"readyReplicas"`
	Status       string `json:"status"` // healthy, degraded, failed
}

// AppStatus represents unified status of an app
type AppStatus struct {
	App            string        `json:"app"`
	TotalClusters  int           `json:"totalClusters"`
	HealthyClusters int          `json:"healthyClusters"`
	TotalReplicas  int32         `json:"totalReplicas"`
	ReadyReplicas  int32         `json:"readyReplicas"`
	OverallStatus  string        `json:"overallStatus"` // healthy, degraded, failed
	Instances      []AppInstance `json:"instances"`
	Issues         []string      `json:"issues,omitempty"`
}

// LogEntry represents a log line with cluster context
type LogEntry struct {
	Cluster   string `json:"cluster"`
	Pod       string `json:"pod"`
	Container string `json:"container,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
	Message   string `json:"message"`
}

// handleGetAppInstances finds all instances of an app across clusters
func (s *Server) handleGetAppInstances(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		App       string `json:"app"`
		Namespace string `json:"namespace"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	results, err := s.executor.Execute(ctx, "", func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
		return s.findAppInCluster(ctx, client, clusterName, params.App, params.Namespace)
	})
	if err != nil {
		return nil, err
	}

	// Flatten results
	var instances []AppInstance
	for _, result := range results {
		if result.Error != "" {
			continue
		}
		if clusterInstances, ok := result.Result.([]AppInstance); ok {
			instances = append(instances, clusterInstances...)
		}
	}

	return map[string]interface{}{
		"app":       params.App,
		"instances": instances,
		"count":     len(instances),
	}, nil
}

// findAppInCluster searches for an app in a single cluster
func (s *Server) findAppInCluster(ctx context.Context, client *kubernetes.Clientset, clusterName, appName, namespace string) ([]AppInstance, error) {
	var instances []AppInstance
	ns := namespace
	if ns == "" {
		ns = metav1.NamespaceAll
	}

	// Search Deployments
	deployments, err := client.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
	if err == nil {
		for _, d := range deployments.Items {
			if matchesApp(d.Name, d.Labels, appName) {
				instances = append(instances, AppInstance{
					Cluster:       clusterName,
					Namespace:     d.Namespace,
					Name:          d.Name,
					Kind:          "Deployment",
					Replicas:      *d.Spec.Replicas,
					ReadyReplicas: d.Status.ReadyReplicas,
					Status:        getDeploymentStatus(&d),
				})
			}
		}
	}

	// Search StatefulSets
	statefulsets, err := client.AppsV1().StatefulSets(ns).List(ctx, metav1.ListOptions{})
	if err == nil {
		for _, s := range statefulsets.Items {
			if matchesApp(s.Name, s.Labels, appName) {
				instances = append(instances, AppInstance{
					Cluster:       clusterName,
					Namespace:     s.Namespace,
					Name:          s.Name,
					Kind:          "StatefulSet",
					Replicas:      *s.Spec.Replicas,
					ReadyReplicas: s.Status.ReadyReplicas,
					Status:        getStatefulSetStatus(&s),
				})
			}
		}
	}

	// Search DaemonSets
	daemonsets, err := client.AppsV1().DaemonSets(ns).List(ctx, metav1.ListOptions{})
	if err == nil {
		for _, d := range daemonsets.Items {
			if matchesApp(d.Name, d.Labels, appName) {
				instances = append(instances, AppInstance{
					Cluster:       clusterName,
					Namespace:     d.Namespace,
					Name:          d.Name,
					Kind:          "DaemonSet",
					Replicas:      d.Status.DesiredNumberScheduled,
					ReadyReplicas: d.Status.NumberReady,
					Status:        getDaemonSetStatus(&d),
				})
			}
		}
	}

	return instances, nil
}

// handleGetAppStatus returns unified status of an app
func (s *Server) handleGetAppStatus(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		App       string `json:"app"`
		Namespace string `json:"namespace"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	results, err := s.executor.Execute(ctx, "", func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
		return s.findAppInCluster(ctx, client, clusterName, params.App, params.Namespace)
	})
	if err != nil {
		return nil, err
	}

	// Aggregate status
	status := AppStatus{
		App: params.App,
	}

	for _, result := range results {
		if result.Error != "" {
			status.Issues = append(status.Issues, fmt.Sprintf("%s: %s", result.Cluster, result.Error))
			continue
		}

		instances, ok := result.Result.([]AppInstance)
		if !ok || len(instances) == 0 {
			continue
		}

		status.TotalClusters++
		clusterHealthy := true

		for _, instance := range instances {
			status.TotalReplicas += instance.Replicas
			status.ReadyReplicas += instance.ReadyReplicas
			status.Instances = append(status.Instances, instance)

			if instance.Status != "healthy" {
				clusterHealthy = false
				status.Issues = append(status.Issues, fmt.Sprintf("%s/%s: %s", instance.Cluster, instance.Name, instance.Status))
			}
		}

		if clusterHealthy {
			status.HealthyClusters++
		}
	}

	// Determine overall status
	if status.TotalClusters == 0 {
		status.OverallStatus = "not found"
	} else if status.HealthyClusters == status.TotalClusters {
		status.OverallStatus = "healthy"
	} else if status.HealthyClusters > 0 {
		status.OverallStatus = "degraded"
	} else {
		status.OverallStatus = "failed"
	}

	return status, nil
}

// handleGetAppLogs returns aggregated logs from an app
func (s *Server) handleGetAppLogs(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		App       string `json:"app"`
		Namespace string `json:"namespace"`
		Tail      int64  `json:"tail"`
		Since     string `json:"since"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if params.Tail == 0 {
		params.Tail = 100
	}

	results, err := s.executor.Execute(ctx, "", func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
		return s.getLogsFromCluster(ctx, client, clusterName, params.App, params.Namespace, params.Tail, params.Since)
	})
	if err != nil {
		return nil, err
	}

	// Aggregate logs
	var allLogs []LogEntry
	for _, result := range results {
		if result.Error != "" {
			continue
		}
		if logs, ok := result.Result.([]LogEntry); ok {
			allLogs = append(allLogs, logs...)
		}
	}

	return map[string]interface{}{
		"app":      params.App,
		"logCount": len(allLogs),
		"logs":     allLogs,
	}, nil
}

// getLogsFromCluster gets logs for an app from a single cluster
func (s *Server) getLogsFromCluster(ctx context.Context, client *kubernetes.Clientset, clusterName, appName, namespace string, tail int64, since string) ([]LogEntry, error) {
	ns := namespace
	if ns == "" {
		ns = metav1.NamespaceAll
	}

	// Find pods matching app
	pods, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var logs []LogEntry
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, pod := range pods.Items {
		if !matchesApp(pod.Name, pod.Labels, appName) {
			continue
		}

		for _, container := range pod.Spec.Containers {
			wg.Add(1)
			go func(podName, containerName, podNamespace string) {
				defer wg.Done()

				opts := &corev1.PodLogOptions{
					Container: containerName,
					TailLines: &tail,
				}

				if since != "" {
					if duration, err := time.ParseDuration(since); err == nil {
						sinceTime := metav1.NewTime(time.Now().Add(-duration))
						opts.SinceTime = &sinceTime
					}
				}

				req := client.CoreV1().Pods(podNamespace).GetLogs(podName, opts)
				stream, err := req.Stream(ctx)
				if err != nil {
					return
				}
				defer stream.Close()

				buf := new(bytes.Buffer)
				_, err = io.Copy(buf, stream)
				if err != nil {
					return
				}

				lines := strings.Split(buf.String(), "\n")
				mu.Lock()
				for _, line := range lines {
					if line == "" {
						continue
					}
					logs = append(logs, LogEntry{
						Cluster:   clusterName,
						Pod:       podName,
						Container: containerName,
						Message:   line,
					})
				}
				mu.Unlock()
			}(pod.Name, container.Name, pod.Namespace)
		}
	}

	wg.Wait()
	return logs, nil
}

// matchesApp checks if a resource matches the app name
func matchesApp(name string, labels map[string]string, appName string) bool {
	// Check common app labels
	if labels["app"] == appName ||
		labels["app.kubernetes.io/name"] == appName ||
		labels["app.kubernetes.io/instance"] == appName {
		return true
	}
	// Fallback to name contains
	return strings.Contains(name, appName)
}

// getDeploymentStatus returns status for a deployment
func getDeploymentStatus(d *appsv1.Deployment) string {
	if d.Status.ReadyReplicas == *d.Spec.Replicas {
		return "healthy"
	}
	if d.Status.ReadyReplicas > 0 {
		return "degraded"
	}
	return "failed"
}

// getStatefulSetStatus returns status for a statefulset
func getStatefulSetStatus(s *appsv1.StatefulSet) string {
	if s.Status.ReadyReplicas == *s.Spec.Replicas {
		return "healthy"
	}
	if s.Status.ReadyReplicas > 0 {
		return "degraded"
	}
	return "failed"
}

// getDaemonSetStatus returns status for a daemonset
func getDaemonSetStatus(d *appsv1.DaemonSet) string {
	if d.Status.NumberReady == d.Status.DesiredNumberScheduled {
		return "healthy"
	}
	if d.Status.NumberReady > 0 {
		return "degraded"
	}
	return "failed"
}
