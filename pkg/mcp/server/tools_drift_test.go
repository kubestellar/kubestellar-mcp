package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/gitops"
	"k8s.io/client-go/rest"
)

type fakeManifestReader struct {
	manifests []gitops.Manifest
	err       error
	source    gitops.ManifestSource
	cleaned   bool
}

func (f *fakeManifestReader) ReadFromGit(_ context.Context, source gitops.ManifestSource) ([]gitops.Manifest, error) {
	f.source = source
	if f.err != nil {
		return nil, f.err
	}
	return f.manifests, nil
}

func (f *fakeManifestReader) Cleanup() {
	f.cleaned = true
}

type fakeDriftDetector struct {
	clusterScopedKinds map[string]bool
	drifts             []gitops.DriftResult
	err                error
	called             bool
	receivedManifests  []gitops.Manifest
	receivedCluster    string
}

func (f *fakeDriftDetector) IsManifestClusterScoped(manifest gitops.Manifest) bool {
	return f.clusterScopedKinds[manifest.Kind]
}

func (f *fakeDriftDetector) DetectDrift(_ context.Context, manifests []gitops.Manifest, clusterName string) ([]gitops.DriftResult, error) {
	f.called = true
	f.receivedManifests = append([]gitops.Manifest(nil), manifests...)
	f.receivedCluster = clusterName
	if f.err != nil {
		return nil, f.err
	}
	return f.drifts, nil
}

func TestToolDetectDrift(t *testing.T) {
	t.Run("missing repo_url", func(t *testing.T) {
		result, rpcErr := callTool(t, &Server{}, "detect_drift", map[string]interface{}{})
		if rpcErr != nil {
			t.Fatalf("unexpected RPC error: %v", rpcErr)
		}
		if !result.IsError {
			t.Fatal("expected tool error for missing repo_url")
		}
		if !strings.Contains(result.Content[0].Text, "repo_url is required") {
			t.Fatalf("unexpected error text: %s", result.Content[0].Text)
		}
	})

	t.Run("empty manifests", func(t *testing.T) {
		reader := &fakeManifestReader{}
		detectorCreated := false
		server := &Server{
			restConfigFactory: func(clusterName string) (*rest.Config, error) {
				return &rest.Config{Host: "https://cluster.example"}, nil
			},
			manifestReaderFactory: func() manifestReader {
				return reader
			},
			driftDetectorFactory: func(config *rest.Config) (driftDetector, error) {
				detectorCreated = true
				return &fakeDriftDetector{}, nil
			},
		}

		result, rpcErr := callTool(t, server, "detect_drift", map[string]interface{}{
			"repo_url": "https://github.com/example/configs",
			"path":     "clusters/dev",
			"branch":   "main",
		})
		if rpcErr != nil {
			t.Fatalf("unexpected RPC error: %v", rpcErr)
		}
		if result.IsError {
			t.Fatalf("expected success result, got error: %s", result.Content[0].Text)
		}
		if !strings.Contains(result.Content[0].Text, "No manifests found in https://github.com/example/configs (path: clusters/dev)") {
			t.Fatalf("unexpected output: %s", result.Content[0].Text)
		}
		if detectorCreated {
			t.Fatal("drift detector should not be created when no manifests are found")
		}
		if !reader.cleaned {
			t.Fatal("expected manifest reader Cleanup to be called")
		}
		if reader.source.Repo != "https://github.com/example/configs" || reader.source.Path != "clusters/dev" || reader.source.Branch != "main" {
			t.Fatalf("unexpected manifest source: %#v", reader.source)
		}
	})

	t.Run("filters namespace and emits no-drift json", func(t *testing.T) {
		reader := &fakeManifestReader{
			manifests: []gitops.Manifest{
				{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Metadata:   gitops.ManifestMetadata{Name: "frontend", Namespace: "apps"},
				},
				{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Metadata:   gitops.ManifestMetadata{Name: "skip-me", Namespace: "other"},
				},
				{
					APIVersion: "rbac.authorization.k8s.io/v1",
					Kind:       "ClusterRole",
					Metadata:   gitops.ManifestMetadata{Name: "read-all"},
				},
			},
		}
		detector := &fakeDriftDetector{
			clusterScopedKinds: map[string]bool{"ClusterRole": true},
		}
		server := &Server{
			restConfigFactory: func(clusterName string) (*rest.Config, error) {
				if clusterName != "" {
					t.Fatalf("restConfigFactory cluster = %q, want empty current context", clusterName)
				}
				return &rest.Config{Host: "https://cluster.example"}, nil
			},
			manifestReaderFactory: func() manifestReader {
				return reader
			},
			driftDetectorFactory: func(config *rest.Config) (driftDetector, error) {
				if config.Host != "https://cluster.example" {
					t.Fatalf("driftDetectorFactory host = %q", config.Host)
				}
				return detector, nil
			},
		}

		result, rpcErr := callTool(t, server, "detect_drift", map[string]interface{}{
			"repo_url":  "https://github.com/example/configs",
			"namespace": "apps",
		})
		if rpcErr != nil {
			t.Fatalf("unexpected RPC error: %v", rpcErr)
		}
		if result.IsError {
			t.Fatalf("expected success result, got error: %s", result.Content[0].Text)
		}
		if !detector.called {
			t.Fatal("expected DetectDrift to be called")
		}
		if detector.receivedCluster != "current-context" {
			t.Fatalf("received cluster = %q, want current-context", detector.receivedCluster)
		}
		if len(detector.receivedManifests) != 2 {
			t.Fatalf("received %d manifests, want 2", len(detector.receivedManifests))
		}
		if detector.receivedManifests[0].Metadata.Name != "frontend" || detector.receivedManifests[1].Metadata.Name != "read-all" {
			t.Fatalf("unexpected filtered manifests: %#v", detector.receivedManifests)
		}
		if !strings.Contains(result.Content[0].Text, "✅ **No drift detected**") {
			t.Fatalf("expected no-drift output, got: %s", result.Content[0].Text)
		}

		start := strings.Index(result.Content[0].Text, "```json\n")
		end := strings.LastIndex(result.Content[0].Text, "\n```")
		if start == -1 || end == -1 || end <= start+8 {
			t.Fatalf("expected JSON code block in output, got: %s", result.Content[0].Text)
		}

		var payload struct {
			Drifted   bool           `json:"drifted"`
			Resources []interface{}  `json:"resources"`
			Summary   map[string]int `json:"summary"`
		}
		if err := json.Unmarshal([]byte(result.Content[0].Text[start+8:end]), &payload); err != nil {
			t.Fatalf("failed to decode JSON payload: %v", err)
		}
		if payload.Drifted {
			t.Fatal("expected drifted=false")
		}
		if len(payload.Resources) != 0 {
			t.Fatalf("expected no resources, got %#v", payload.Resources)
		}
		wantSummary := map[string]int{"total": 2, "synced": 2, "drifted": 0, "missing": 0, "modified": 0}
		for key, want := range wantSummary {
			if payload.Summary[key] != want {
				t.Fatalf("summary[%q] = %d, want %d", key, payload.Summary[key], want)
			}
		}
		if !reader.cleaned {
			t.Fatal("expected manifest reader Cleanup to be called")
		}
	})

	t.Run("cluster config error", func(t *testing.T) {
		server := &Server{
			restConfigFactory: func(clusterName string) (*rest.Config, error) {
				if clusterName != "member1" {
					t.Fatalf("restConfigFactory cluster = %q, want member1", clusterName)
				}
				return nil, errors.New("missing cluster config")
			},
		}

		result, rpcErr := callTool(t, server, "detect_drift", map[string]interface{}{
			"repo_url": "https://github.com/example/configs",
			"cluster":  "member1",
		})
		if rpcErr != nil {
			t.Fatalf("unexpected RPC error: %v", rpcErr)
		}
		if !result.IsError {
			t.Fatal("expected tool error for failed cluster config")
		}
		if !strings.Contains(result.Content[0].Text, "Failed to create client config") || !strings.Contains(result.Content[0].Text, "missing cluster config") {
			t.Fatalf("unexpected error text: %s", result.Content[0].Text)
		}
	})
}
