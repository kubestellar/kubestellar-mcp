package mcp

import (
	"testing"

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
			if got := matchesApp(tt.resource, tt.labels, tt.appName); got != tt.wantMatch {
				t.Fatalf("matchesApp() = %v, want %v", got, tt.wantMatch)
			}
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
		{name: "deployment healthy", want: "healthy", got: getDeploymentStatus(&appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: &replicas}, Status: appsv1.DeploymentStatus{ReadyReplicas: 3}})},
		{name: "deployment degraded", want: "degraded", got: getDeploymentStatus(&appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: &replicas}, Status: appsv1.DeploymentStatus{ReadyReplicas: 1}})},
		{name: "deployment failed", want: "failed", got: getDeploymentStatus(&appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: &replicas}, Status: appsv1.DeploymentStatus{ReadyReplicas: 0}})},
		{name: "statefulset healthy", want: "healthy", got: getStatefulSetStatus(&appsv1.StatefulSet{Spec: appsv1.StatefulSetSpec{Replicas: &replicas}, Status: appsv1.StatefulSetStatus{ReadyReplicas: 3}})},
		{name: "statefulset degraded", want: "degraded", got: getStatefulSetStatus(&appsv1.StatefulSet{Spec: appsv1.StatefulSetSpec{Replicas: &replicas}, Status: appsv1.StatefulSetStatus{ReadyReplicas: 1}})},
		{name: "daemonset healthy", want: "healthy", got: getDaemonSetStatus(&appsv1.DaemonSet{Status: appsv1.DaemonSetStatus{DesiredNumberScheduled: 4, NumberReady: 4}})},
		{name: "daemonset degraded", want: "degraded", got: getDaemonSetStatus(&appsv1.DaemonSet{Status: appsv1.DaemonSetStatus{DesiredNumberScheduled: 4, NumberReady: 2}})},
		{name: "daemonset failed", want: "failed", got: getDaemonSetStatus(&appsv1.DaemonSet{Status: appsv1.DaemonSetStatus{DesiredNumberScheduled: 4, NumberReady: 0}})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("status = %q, want %q", tt.got, tt.want)
			}
		})
	}
}
