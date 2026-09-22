package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	nsval "github.com/kubestellar/kubestellar-mcp/pkg/security/namespace"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/kubestellar/kubestellar-mcp/pkg/ai/claude"
)

// AppInstance represents an app instance in a cluster
type AppInstance struct {
	Cluster       string `json:"cluster"`
	Namespace     string `json:"namespace"`
	Name          string `json:"name"`
	Kind          string `json:"kind"` // Deployment, StatefulSet, DaemonSet
	Replicas      int32  `json:"replicas"`
	ReadyReplicas int32  `json:"readyReplicas"`
	Status        string `json:"status"` // healthy, degraded, failed
}

// AppStatus represents unified status of an app
type AppStatus struct {
	App             string        `json:"app"`
	TotalClusters   int           `json:"totalClusters"`
	HealthyClusters int           `json:"healthyClusters"`
	// UncheckedClusters counts clusters that could not be queried at all
	// (connectivity failure, RBAC denial, timeout, etc.). These clusters are
	// excluded from TotalClusters/HealthyClusters because we have no
	// instance data for them, but their presence must still prevent
	// OverallStatus from reporting "healthy" — an app cannot be confirmed
	// healthy on a cluster nobody was able to check.
	UncheckedClusters int           `json:"uncheckedClusters,omitempty"`
	TotalReplicas     int32         `json:"totalReplicas"`
	ReadyReplicas     int32         `json:"readyReplicas"`
	OverallStatus     string        `json:"overallStatus"` // healthy, degraded, failed, unknown, not found
	Instances         []AppInstance `json:"instances"`
	Issues            []string      `json:"issues,omitempty"`
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
		Namespace string `json:"namespace,omitempty"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if err := claude.ValidateK8sName(params.App); err != nil {
		return nil, fmt.Errorf("invalid app name: %w", err)
	}
	if params.Namespace != "" {
		if err := nsval.ValidateNamespace(params.Namespace); err != nil {
			return nil, fmt.Errorf("invalid namespace: %w", err)
		}
	}

	results, err := s.executor.Execute(ctx, "", func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
		return s.findAppInCluster(ctx, client, clusterName, params.App, params.Namespace)
	})
	if err != nil {
		return nil, err
	}

	// Flatten results. Clusters whose lookup failed (connectivity, RBAC,
	// timeout, etc.) are recorded in uncheckedClusters instead of being
	// silently dropped: without this, a cluster hosting the only running
	// instance of an app that happens to be unreachable would make this
	// tool report "count: 0" — indistinguishable from the app genuinely
	// not being deployed anywhere (same false-negative class fixed for
	// handleGetAppStatus's overallStatus).
	var instances []AppInstance
	var uncheckedClusters []string
	for _, result := range results {
		if result.Error != "" {
			uncheckedClusters = append(uncheckedClusters, result.Cluster)
			continue
		}
		if clusterInstances, ok := result.Result.([]AppInstance); ok {
			instances = append(instances, clusterInstances...)
		}
	}

	response := map[string]interface{}{
		"app":       claude.SanitizeForPrompt(params.App),
		"instances": instances,
		"count":     len(instances),
	}
	if len(uncheckedClusters) > 0 {
		response["uncheckedClusters"] = uncheckedClusters
	}

	return response, nil
}

// findAppInCluster searches for an app in a single cluster
func (s *Server) findAppInCluster(ctx context.Context, client *kubernetes.Clientset, clusterName, appName, namespace string) ([]AppInstance, error) {
	var instances []AppInstance
	ns := namespace
	if ns == "" {
		ns = metav1.NamespaceAll
	}

	if ns != "" {
		if err := nsval.ValidateNamespace(ns); err != nil {
			return nil, fmt.Errorf("invalid namespace: %w", err)
		}
	}

	// Search Deployments, StatefulSets, and DaemonSets. A List failure on any
	// one of these (RBAC denial, API server unreachable, context timeout,
	// etc.) must not be swallowed: silently treating it as "no instances
	// found" lets a cluster that couldn't actually be checked count as
	// healthy (or drop out of the status entirely) in handleGetAppStatus's
	// aggregation, which can misreport overall app health as "healthy" while
	// one cluster's real state is unknown. Collect and surface every error
	// instead so the caller can distinguish "not deployed here" from
	// "couldn't check".
	var listErrs []error

	deployments, err := client.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		listErrs = append(listErrs, fmt.Errorf("list deployments: %w", err))
	} else {
		for _, d := range deployments.Items {
			if matchesApp(d.Name, d.Labels, appName) {
				instances = append(instances, AppInstance{
					Cluster:       clusterName,
					Namespace:     d.Namespace,
					Name:          d.Name,
					Kind:          "Deployment",
					Replicas:      replicasOrDefault(d.Spec.Replicas),
					ReadyReplicas: d.Status.ReadyReplicas,
					Status:        getDeploymentStatus(&d),
				})
			}
		}
	}

	statefulsets, err := client.AppsV1().StatefulSets(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		listErrs = append(listErrs, fmt.Errorf("list statefulsets: %w", err))
	} else {
		for _, s := range statefulsets.Items {
			if matchesApp(s.Name, s.Labels, appName) {
				instances = append(instances, AppInstance{
					Cluster:       clusterName,
					Namespace:     s.Namespace,
					Name:          s.Name,
					Kind:          "StatefulSet",
					Replicas:      replicasOrDefault(s.Spec.Replicas),
					ReadyReplicas: s.Status.ReadyReplicas,
					Status:        getStatefulSetStatus(&s),
				})
			}
		}
	}

	daemonsets, err := client.AppsV1().DaemonSets(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		listErrs = append(listErrs, fmt.Errorf("list daemonsets: %w", err))
	} else {
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

	if len(listErrs) > 0 {
		return instances, errors.Join(listErrs...)
	}
	return instances, nil
}

// handleGetAppStatus returns unified status of an app
func (s *Server) handleGetAppStatus(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		App       string `json:"app"`
		Namespace string `json:"namespace,omitempty"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if err := claude.ValidateK8sName(params.App); err != nil {
		return nil, fmt.Errorf("invalid app name: %w", err)
	}
	if params.Namespace != "" {
		if err := nsval.ValidateNamespace(params.Namespace); err != nil {
			return nil, fmt.Errorf("invalid namespace: %w", err)
		}
	}

	results, err := s.executor.Execute(ctx, "", func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
		return s.findAppInCluster(ctx, client, clusterName, params.App, params.Namespace)
	})
	if err != nil {
		return nil, err
	}

	// Aggregate status
	status := AppStatus{
		App: claude.SanitizeForPrompt(params.App),
	}

	for _, result := range results {
		if result.Error != "" {
			status.Issues = append(status.Issues, fmt.Sprintf("%s: could not check (%s)", result.Cluster, result.Error))
			status.UncheckedClusters++
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

	// Determine overall status. A cluster that could not be checked
	// (status.UncheckedClusters) must never be treated as evidence of
	// health: it is a dependency this tool failed to observe, not a
	// dependency confirmed absent or healthy.
	switch {
	case status.TotalClusters == 0 && status.UncheckedClusters == 0:
		status.OverallStatus = "not found"
	case status.TotalClusters == 0:
		// Every cluster we tried to check failed; the app may or may not be
		// deployed anywhere, but we have zero verified data either way.
		status.OverallStatus = "unknown"
	case status.HealthyClusters == status.TotalClusters && status.UncheckedClusters == 0:
		status.OverallStatus = "healthy"
	case status.HealthyClusters == status.TotalClusters:
		// All checkable clusters are healthy, but at least one cluster
		// could not be verified at all, so we cannot claim full health.
		status.OverallStatus = "degraded"
	case status.HealthyClusters > 0:
		status.OverallStatus = "degraded"
	default:
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

	if err := claude.ValidateK8sName(params.App); err != nil {
		return nil, fmt.Errorf("invalid app name: %w", err)
	}
	if params.Namespace != "" {
		if err := nsval.ValidateNamespace(params.Namespace); err != nil {
			return nil, fmt.Errorf("invalid namespace: %w", err)
		}
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
		"app":      claude.SanitizeForPrompt(params.App),
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

	if ns != "" {
		if err := nsval.ValidateNamespace(ns); err != nil {
			return nil, fmt.Errorf("invalid namespace: %w", err)
		}
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
				defer func() {
					_ = stream.Close()
				}()

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

func replicasOrDefault(replicas *int32) int32 {
	const defaultReplicas int32 = 1
	if replicas == nil {
		return defaultReplicas
	}
	return *replicas
}

// getDeploymentStatus returns status for a deployment
func getDeploymentStatus(d *appsv1.Deployment) string {
	if d.Status.ReadyReplicas == replicasOrDefault(d.Spec.Replicas) {
		return "healthy"
	}
	if d.Status.ReadyReplicas > 0 {
		return "degraded"
	}
	return "failed"
}

// getStatefulSetStatus returns status for a statefulset
func getStatefulSetStatus(s *appsv1.StatefulSet) string {
	if s.Status.ReadyReplicas == replicasOrDefault(s.Spec.Replicas) {
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

// appToolDefs returns the tool definitions handled by this file.
func (s *Server) appToolDefs() []toolDef {
	return []toolDef{
		{
			Name:        "get_app_instances",
			Description: "Find all instances of an app across all clusters. Returns where the app is running, replica counts, and health status.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"app": map[string]interface{}{
						"type":        "string",
						"description": "App name to search for (matches label app=<name> or name contains <name>)",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Namespace to search in (all namespaces if not specified)",
					},
				},
				"required": []string{"app"},
			},
			Handler: s.handleGetAppInstances,
		},
		{
			Name:        "get_app_status",
			Description: "Get unified status of an app across all clusters. Shows health (healthy/degraded/failed), replica counts, and any issues.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"app": map[string]interface{}{
						"type":        "string",
						"description": "App name",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Namespace (all namespaces if not specified)",
					},
				},
				"required": []string{"app"},
			},
			Handler: s.handleGetAppStatus,
		},
		{
			Name:        "get_app_logs",
			Description: "Get aggregated logs from an app across all clusters. Logs are labeled with cluster name for easy identification.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"app": map[string]interface{}{
						"type":        "string",
						"description": "App name",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Namespace (all namespaces if not specified)",
					},
					"tail": map[string]interface{}{
						"type":        "integer",
						"description": "Number of lines from end (default 100)",
					},
					"since": map[string]interface{}{
						"type":        "string",
						"description": "Only return logs newer than duration (e.g., 1h, 30m)",
					},
				},
				"required": []string{"app"},
			},
			Handler: s.handleGetAppLogs,
		},
	}
}
