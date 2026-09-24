package app

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
)

func TestMatchesApp(t *testing.T) {
	tests := []struct {
		name      string
		resource  string
		labels    map[string]string
		appName   string
		wantMatch bool
	}{
		{name: "matches app label", resource: "demo", labels: map[string]string{"app": "guestbook"}, appName: "guestbook", wantMatch: true},
		{name: "matches kubernetes name label", resource: "demo", labels: map[string]string{"app.kubernetes.io/name": "guestbook"}, appName: "guestbook", wantMatch: true},
		{name: "matches instance label", resource: "demo", labels: map[string]string{"app.kubernetes.io/instance": "guestbook"}, appName: "guestbook", wantMatch: true},
		{name: "matches resource name substring", resource: "guestbook-api", labels: map[string]string{}, appName: "guestbook", wantMatch: true},
		{name: "does not match", resource: "payments", labels: map[string]string{"app": "billing"}, appName: "guestbook", wantMatch: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantMatch, MatchesApp(tt.resource, tt.labels, tt.appName))
		})
	}
}

func TestWorkloadStatusHelpers(t *testing.T) {
	replicas := int32(3)
	tests := []struct {
		name string
		want string
		got  string
	}{
		{name: "deployment healthy", want: "healthy", got: GetDeploymentStatus(&appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: &replicas}, Status: appsv1.DeploymentStatus{ReadyReplicas: 3}})},
		{name: "deployment degraded", want: "degraded", got: GetDeploymentStatus(&appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: &replicas}, Status: appsv1.DeploymentStatus{ReadyReplicas: 1}})},
		{name: "deployment failed", want: "failed", got: GetDeploymentStatus(&appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: &replicas}, Status: appsv1.DeploymentStatus{ReadyReplicas: 0}})},
		{name: "deployment nil replicas healthy", want: "healthy", got: GetDeploymentStatus(&appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: nil}, Status: appsv1.DeploymentStatus{ReadyReplicas: 1}})},
		{name: "deployment nil replicas failed", want: "failed", got: GetDeploymentStatus(&appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: nil}, Status: appsv1.DeploymentStatus{ReadyReplicas: 0}})},
		{name: "statefulset healthy", want: "healthy", got: GetStatefulSetStatus(&appsv1.StatefulSet{Spec: appsv1.StatefulSetSpec{Replicas: &replicas}, Status: appsv1.StatefulSetStatus{ReadyReplicas: 3}})},
		{name: "statefulset degraded", want: "degraded", got: GetStatefulSetStatus(&appsv1.StatefulSet{Spec: appsv1.StatefulSetSpec{Replicas: &replicas}, Status: appsv1.StatefulSetStatus{ReadyReplicas: 1}})},
		{name: "statefulset failed", want: "failed", got: GetStatefulSetStatus(&appsv1.StatefulSet{Spec: appsv1.StatefulSetSpec{Replicas: &replicas}, Status: appsv1.StatefulSetStatus{ReadyReplicas: 0}})},
		{name: "statefulset nil replicas healthy", want: "healthy", got: GetStatefulSetStatus(&appsv1.StatefulSet{Spec: appsv1.StatefulSetSpec{Replicas: nil}, Status: appsv1.StatefulSetStatus{ReadyReplicas: 1}})},
		{name: "statefulset nil replicas failed", want: "failed", got: GetStatefulSetStatus(&appsv1.StatefulSet{Spec: appsv1.StatefulSetSpec{Replicas: nil}, Status: appsv1.StatefulSetStatus{ReadyReplicas: 0}})},
		{name: "daemonset healthy", want: "healthy", got: GetDaemonSetStatus(&appsv1.DaemonSet{Status: appsv1.DaemonSetStatus{DesiredNumberScheduled: 4, NumberReady: 4}})},
		{name: "daemonset degraded", want: "degraded", got: GetDaemonSetStatus(&appsv1.DaemonSet{Status: appsv1.DaemonSetStatus{DesiredNumberScheduled: 4, NumberReady: 2}})},
		{name: "daemonset failed", want: "failed", got: GetDaemonSetStatus(&appsv1.DaemonSet{Status: appsv1.DaemonSetStatus{DesiredNumberScheduled: 4, NumberReady: 0}})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.got)
		})
	}
}

func TestReplicasOrDefault(t *testing.T) {
	tests := []struct {
		name     string
		replicas *int32
		want     int32
	}{
		{name: "nil returns default 1", replicas: nil, want: 1},
		{name: "non-nil returns value", replicas: int32Ptr(3), want: 3},
		{name: "zero value returns 0", replicas: int32Ptr(0), want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ReplicasOrDefault(tt.replicas))
		})
	}
}

func TestTools(t *testing.T) {
	executor := &fakeExecutor{}
	defs := Tools(executor)
	require.Len(t, defs, 3)
	assert.Equal(t, []string{"get_app_instances", "get_app_status", "get_app_logs"}, []string{defs[0].Name, defs[1].Name, defs[2].Name})

	for _, def := range defs {
		res, err := def.Handler(context.Background(), mustMarshalJSON(t, map[string]interface{}{"app": "demo"}))
		require.NoError(t, err)
		switch def.Name {
		case "get_app_instances":
			decoded := decodeAppInstancesResult(t, res)
			assert.Equal(t, "demo", decoded.App)
			assert.Zero(t, decoded.Count)
		case "get_app_status":
			decoded := decodeAppStatusResult(t, res)
			assert.Equal(t, "demo", decoded.App)
			assert.Equal(t, "not found", decoded.OverallStatus)
		case "get_app_logs":
			decoded, ok := res.(map[string]interface{})
			require.True(t, ok)
			assert.Equal(t, "demo", decoded["app"])
			assert.Equal(t, 0, decoded["logCount"])
		}
	}
}
