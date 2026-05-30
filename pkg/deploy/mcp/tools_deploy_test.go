package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
)

func TestHandleListClusterCapabilitiesValidation(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
	})
	// Initialize executor and selector
	server.executor = multicluster.NewExecutor(server.manager)
	server.selector = multicluster.NewSelector(server.executor)

	tests := []struct {
		name    string
		args    map[string]interface{}
		wantErr bool
	}{
		{
			name:    "nil args",
			args:    nil,
			wantErr: false, // Should handle nil args gracefully
		},
		{
			name:    "empty args",
			args:    map[string]interface{}{},
			wantErr: false, // Should return all cluster capabilities
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var args json.RawMessage
			if tt.args != nil {
				args = mustMarshalJSON(t, tt.args)
			}

			_, err := server.handleListClusterCapabilities(context.Background(), args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("handleListClusterCapabilities() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHandleFindClustersForWorkloadValidation(t *testing.T) {
	server := newHelmTestServer(t, nil)
	// Initialize executor and selector
	server.executor = multicluster.NewExecutor(server.manager)
	server.selector = multicluster.NewSelector(server.executor)

	tests := []struct {
		name    string
		args    map[string]interface{}
		wantErr bool
	}{
		{
			name:    "invalid json",
			args:    nil,
			wantErr: true,
		},
		{
			name: "valid args with GPU requirements",
			args: map[string]interface{}{
				"gpu_type": "nvidia-tesla-v100",
				"min_gpu":  2,
			},
			wantErr: false, // Should execute successfully
		},
		{
			name: "valid args with memory requirements",
			args: map[string]interface{}{
				"min_memory": "16Gi",
				"min_cpu":    "4",
			},
			wantErr: false, // Should execute successfully
		},
		{
			name: "valid args with labels",
			args: map[string]interface{}{
				"labels": map[string]string{
					"region": "us-west",
				},
			},
			wantErr: false, // Should execute successfully
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var args json.RawMessage
			if tt.args != nil {
				args = mustMarshalJSON(t, tt.args)
			} else {
				args = json.RawMessage(`{invalid json`)
			}

			_, err := server.handleFindClustersForWorkload(context.Background(), args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("handleFindClustersForWorkload() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHandleFindClustersForWorkloadInvalidJSON(t *testing.T) {
	server := newHelmTestServer(t, nil)
	// Initialize executor and selector
	server.executor = multicluster.NewExecutor(server.manager)
	server.selector = multicluster.NewSelector(server.executor)

	_, err := server.handleFindClustersForWorkload(context.Background(), json.RawMessage(`{invalid`))
	if err == nil {
		t.Fatalf("handleFindClustersForWorkload() expected error for invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "invalid arguments") {
		t.Fatalf("handleFindClustersForWorkload() error = %v, want error containing 'invalid arguments'", err)
	}
}

func TestHandleDeployAppValidation(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
	})
	// Initialize executor and selector
	server.executor = multicluster.NewExecutor(server.manager)
	server.selector = multicluster.NewSelector(server.executor)

	tests := []struct {
		name    string
		args    map[string]interface{}
		wantErr string
	}{
		{
			name:    "invalid json",
			args:    nil,
			wantErr: "invalid arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var args json.RawMessage
			if tt.args != nil {
				args = mustMarshalJSON(t, tt.args)
			} else {
				args = json.RawMessage(`{invalid`)
			}

			_, err := server.handleDeployApp(context.Background(), args)
			if err == nil {
				t.Fatalf("handleDeployApp() expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("handleDeployApp() error = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestHandleDeployAppDryRun(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
	})
	// Initialize executor and selector
	server.executor = multicluster.NewExecutor(server.manager)
	server.selector = multicluster.NewSelector(server.executor)

	manifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: demo
  namespace: default
data:
  key: value
`

	args := mustMarshalJSON(t, map[string]interface{}{
		"manifest": manifest,
		"clusters": []string{"alpha"},
		"dry_run":  true,
	})

	got, err := server.handleDeployApp(context.Background(), args)
	if err != nil {
		t.Fatalf("handleDeployApp() error = %v", err)
	}

	result := got.(map[string]interface{})
	if !result["dryRun"].(bool) {
		t.Fatalf("dryRun = %v, want true", result["dryRun"])
	}

	targetClusters := result["targetClusters"].([]string)
	if len(targetClusters) != 1 || targetClusters[0] != "alpha" {
		t.Fatalf("targetClusters = %v, want [alpha]", targetClusters)
	}

	deployResults := result["results"].([]DeployResult)
	if len(deployResults) != 1 {
		t.Fatalf("results count = %d, want 1", len(deployResults))
	}

	if deployResults[0].Status != "would-apply" {
		t.Fatalf("status = %q, want would-apply", deployResults[0].Status)
	}
	if !strings.Contains(deployResults[0].Resource, "ConfigMap") {
		t.Fatalf("resource = %q, want ConfigMap", deployResults[0].Resource)
	}
}

func TestHandleScaleAppValidation(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
	})
	// Initialize executor and selector
	server.executor = multicluster.NewExecutor(server.manager)
	server.selector = multicluster.NewSelector(server.executor)

	tests := []struct {
		name    string
		args    map[string]interface{}
		wantErr string
	}{
		{
			name:    "invalid json",
			args:    nil,
			wantErr: "invalid arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var args json.RawMessage
			if tt.args != nil {
				args = mustMarshalJSON(t, tt.args)
			} else {
				args = json.RawMessage(`{invalid`)
			}

			_, err := server.handleScaleApp(context.Background(), args)
			if err == nil {
				t.Fatalf("handleScaleApp() expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("handleScaleApp() error = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestHandlePatchAppValidation(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
	})
	// Initialize executor and selector
	server.executor = multicluster.NewExecutor(server.manager)
	server.selector = multicluster.NewSelector(server.executor)

	tests := []struct {
		name    string
		args    map[string]interface{}
		wantErr bool
	}{
		{
			name:    "invalid json",
			args:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var args json.RawMessage
			if tt.args != nil {
				args = mustMarshalJSON(t, tt.args)
			} else {
				args = json.RawMessage(`{invalid`)
			}

			_, err := server.handlePatchApp(context.Background(), args)

			// For valid args, we expect it to execute (may fail on K8s client call, which is expected)
			// For invalid JSON, we expect unmarshal error
			if tt.wantErr && err == nil {
				t.Fatalf("handlePatchApp() expected error, got nil")
			}
			if tt.wantErr && !strings.Contains(err.Error(), "invalid arguments") {
				t.Fatalf("handlePatchApp() error = %v, want error containing 'invalid arguments'", err)
			}
		})
	}
}

func TestApplyManifestMultiDocument(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
	})
	// Initialize executor and selector
	server.executor = multicluster.NewExecutor(server.manager)
	server.selector = multicluster.NewSelector(server.executor)

	// Multi-document YAML with different kinds
	manifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: config1
  namespace: default
data:
  key: value1
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: config2
  namespace: apps
data:
  key: value2
---
apiVersion: v1
kind: Service
metadata:
  name: svc1
  namespace: default
spec:
  ports:
  - port: 80
`

	args := mustMarshalJSON(t, map[string]interface{}{
		"manifest": manifest,
		"clusters": []string{"alpha"},
		"dry_run":  true,
	})

	got, err := server.handleDeployApp(context.Background(), args)
	if err != nil {
		t.Fatalf("handleDeployApp() error = %v", err)
	}

	result := got.(map[string]interface{})
	deployResults := result["results"].([]DeployResult)

	// Should have 3 results: 2 ConfigMaps + 1 Service
	if len(deployResults) != 3 {
		t.Fatalf("results count = %d, want 3", len(deployResults))
	}

	// Check that all are would-apply status
	for _, r := range deployResults {
		if r.Status != "would-apply" {
			t.Fatalf("unexpected status for %s: %s", r.Resource, r.Status)
		}
	}
}

func TestHandleDeployAppWithGPURequirements(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
	})
	// Initialize executor and selector
	server.executor = multicluster.NewExecutor(server.manager)
	server.selector = multicluster.NewSelector(server.executor)

	manifest := `apiVersion: v1
kind: Pod
metadata:
  name: gpu-pod
spec:
  containers:
  - name: cuda
    image: nvidia/cuda:latest
`

	args := mustMarshalJSON(t, map[string]interface{}{
		"manifest": manifest,
		"gpu_type": "nvidia-tesla-v100",
		"min_gpu":  1,
		"dry_run":  true,
	})

	// Should execute successfully (though may not find any GPU clusters)
	_, err := server.handleDeployApp(context.Background(), args)
	// We expect it to either succeed or fail because no clusters match GPU requirements
	if err != nil && !strings.Contains(err.Error(), "no clusters found matching requirements") {
		t.Fatalf("handleDeployApp() unexpected error: %v", err)
	}
}
