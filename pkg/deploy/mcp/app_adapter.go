package mcp

import (
	"context"
	"encoding/json"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/app"
)

// This file adapts the pkg/deploy/mcp/app sub-package (the "app" domain:
// get_app_instances, get_app_status, get_app_logs) into the root Server, per
// epic #983 (decompose pkg/deploy/mcp into per-domain sub-packages). It
// replaces the former tools_app.go, whose logic was moved verbatim into
// pkg/deploy/mcp/app. Behavior, including tools/list registration order
// (registry.go:19-24), is unchanged.
//
// Re-exports below exist solely so in-package tests (tools_app_*_test.go)
// keep compiling against *Server without modification; they are thin
// delegations to the app sub-package, mirroring the pattern used in
// pkg/mcp/server/upgrades.go.

// AppInstance is re-exported from the app sub-package.
type AppInstance = app.AppInstance

// AppStatus is re-exported from the app sub-package.
type AppStatus = app.AppStatus

// LogEntry is re-exported from the app sub-package.
type LogEntry = app.LogEntry

// appToolDefs returns the tool definitions handled by the app sub-package.
func (s *Server) appToolDefs() []toolDef {
	appDefs := app.Tools(s.executor)
	defs := make([]toolDef, 0, len(appDefs))
	for _, d := range appDefs {
		defs = append(defs, toolDef{
			Name:        d.Name,
			Description: d.Description,
			InputSchema: d.InputSchema,
			Handler:     d.Handler,
		})
	}
	return defs
}

// handleGetAppInstances delegates to app.GetAppInstances. Retained on
// *Server for test compatibility.
func (s *Server) handleGetAppInstances(ctx context.Context, args json.RawMessage) (interface{}, error) {
	return app.GetAppInstances(ctx, s.executor, args)
}

// handleGetAppStatus delegates to app.GetAppStatus. Retained on *Server for
// test compatibility.
func (s *Server) handleGetAppStatus(ctx context.Context, args json.RawMessage) (interface{}, error) {
	return app.GetAppStatus(ctx, s.executor, args)
}

// handleGetAppLogs delegates to app.GetAppLogs. Retained on *Server for test
// compatibility.
func (s *Server) handleGetAppLogs(ctx context.Context, args json.RawMessage) (interface{}, error) {
	return app.GetAppLogs(ctx, s.executor, args)
}

// findAppInCluster delegates to app.FindAppInCluster. Retained on *Server
// for test compatibility.
func (s *Server) findAppInCluster(ctx context.Context, client *kubernetes.Clientset, clusterName, appName, namespace string) ([]AppInstance, error) {
	return app.FindAppInCluster(ctx, client, clusterName, appName, namespace)
}

// getLogsFromCluster delegates to app.GetLogsFromCluster. Retained on
// *Server for test compatibility.
func (s *Server) getLogsFromCluster(ctx context.Context, client *kubernetes.Clientset, clusterName, appName, namespace string, tail int64, since string) ([]LogEntry, error) {
	return app.GetLogsFromCluster(ctx, client, clusterName, appName, namespace, tail, since)
}

// matchesApp delegates to app.MatchesApp. Retained as a package-level
// function for test compatibility (tools_app_test.go).
func matchesApp(name string, labels map[string]string, appName string) bool {
	return app.MatchesApp(name, labels, appName)
}

// replicasOrDefault delegates to app.ReplicasOrDefault. Retained for test
// compatibility.
func replicasOrDefault(replicas *int32) int32 {
	return app.ReplicasOrDefault(replicas)
}

// getDeploymentStatus delegates to app.GetDeploymentStatus. Retained for
// test compatibility.
func getDeploymentStatus(d *appsv1.Deployment) string {
	return app.GetDeploymentStatus(d)
}

// getStatefulSetStatus delegates to app.GetStatefulSetStatus. Retained for
// test compatibility.
func getStatefulSetStatus(s *appsv1.StatefulSet) string {
	return app.GetStatefulSetStatus(s)
}

// getDaemonSetStatus delegates to app.GetDaemonSetStatus. Retained for test
// compatibility.
func getDaemonSetStatus(d *appsv1.DaemonSet) string {
	return app.GetDaemonSetStatus(d)
}
