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

// This file complements tools_drift_test.go by covering the branches
// that TestToolDetectDrift does not exercise:
//
//   1. reader.ReadFromGit returns an error
//   2. driftDetectorFactory returns an error (after manifests were read)
//   3. detector.DetectDrift returns an error
//   4. Drift IS detected (both DriftTypeMissing and DriftTypeModified) —
//      verifies the human-readable markdown summary AND the JSON payload
//      shape used by programmatic clients (drifted, summary counts,
//      resources[].kind/name/namespace/driftType/field/differences/gitValue/clusterValue).
//   5. Reader cleanup is called even when detector creation fails
//      (defer reader.Cleanup() branch).
//   6. newManifestReader / newDriftDetector fall back to the real
//      implementations when no factory is set (compile-check plus
//      lightweight sanity).

func TestToolDetectDrift_ReadFromGitError(t *testing.T) {
	reader := &fakeManifestReader{err: errors.New("git clone failed")}
	server := &Server{
		restConfigFactory: func(_ string) (*rest.Config, error) {
			return &rest.Config{Host: "https://cluster.example"}, nil
		},
		manifestReaderFactory: func() manifestReader { return reader },
		driftDetectorFactory: func(_ *rest.Config) (driftDetector, error) {
			t.Fatal("driftDetectorFactory must not be called when ReadFromGit fails")
			return nil, nil
		},
	}

	result, rpcErr := callTool(t, server, "detect_drift", map[string]interface{}{
		"repo_url": "https://github.com/example/configs",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatal("expected tool error when ReadFromGit fails")
	}
	if !strings.Contains(result.Content[0].Text, "Failed to read manifests from git") ||
		!strings.Contains(result.Content[0].Text, "git clone failed") {
		t.Fatalf("unexpected error text: %s", result.Content[0].Text)
	}
	if !reader.cleaned {
		t.Fatal("expected reader.Cleanup to be called via defer")
	}
}

func TestToolDetectDrift_DetectorFactoryError(t *testing.T) {
	reader := &fakeManifestReader{
		manifests: []gitops.Manifest{
			{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Metadata:   gitops.ManifestMetadata{Name: "web", Namespace: "apps"},
			},
		},
	}
	server := &Server{
		restConfigFactory: func(_ string) (*rest.Config, error) {
			return &rest.Config{Host: "https://cluster.example"}, nil
		},
		manifestReaderFactory: func() manifestReader { return reader },
		driftDetectorFactory: func(_ *rest.Config) (driftDetector, error) {
			return nil, errors.New("rest mapper unavailable")
		},
	}

	result, rpcErr := callTool(t, server, "detect_drift", map[string]interface{}{
		"repo_url": "https://github.com/example/configs",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatal("expected tool error when driftDetectorFactory fails")
	}
	if !strings.Contains(result.Content[0].Text, "Failed to create drift detector") ||
		!strings.Contains(result.Content[0].Text, "rest mapper unavailable") {
		t.Fatalf("unexpected error text: %s", result.Content[0].Text)
	}
	if !reader.cleaned {
		t.Fatal("expected reader.Cleanup to be called via defer even when detector creation fails")
	}
}

func TestToolDetectDrift_DetectDriftError(t *testing.T) {
	reader := &fakeManifestReader{
		manifests: []gitops.Manifest{
			{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Metadata:   gitops.ManifestMetadata{Name: "web", Namespace: "apps"},
			},
		},
	}
	detector := &fakeDriftDetector{err: errors.New("api server unreachable")}
	server := &Server{
		restConfigFactory: func(_ string) (*rest.Config, error) {
			return &rest.Config{Host: "https://cluster.example"}, nil
		},
		manifestReaderFactory: func() manifestReader { return reader },
		driftDetectorFactory:  func(_ *rest.Config) (driftDetector, error) { return detector, nil },
	}

	result, rpcErr := callTool(t, server, "detect_drift", map[string]interface{}{
		"repo_url": "https://github.com/example/configs",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatal("expected tool error when DetectDrift fails")
	}
	if !strings.Contains(result.Content[0].Text, "Failed to detect drift") ||
		!strings.Contains(result.Content[0].Text, "api server unreachable") {
		t.Fatalf("unexpected error text: %s", result.Content[0].Text)
	}
	if !detector.called {
		t.Fatal("expected DetectDrift to be invoked")
	}
	if !reader.cleaned {
		t.Fatal("expected reader.Cleanup to be called via defer")
	}
}

func TestToolDetectDrift_DriftPresent_MarkdownAndJSON(t *testing.T) {
	reader := &fakeManifestReader{
		manifests: []gitops.Manifest{
			{Kind: "Deployment", Metadata: gitops.ManifestMetadata{Name: "web", Namespace: "apps"}},
			{Kind: "Service", Metadata: gitops.ManifestMetadata{Name: "web", Namespace: "apps"}},
			{Kind: "ConfigMap", Metadata: gitops.ManifestMetadata{Name: "cfg", Namespace: "apps"}},
		},
	}
	detector := &fakeDriftDetector{
		drifts: []gitops.DriftResult{
			{
				Kind:         "Deployment",
				Name:         "web",
				Namespace:    "apps",
				DriftType:    gitops.DriftTypeModified,
				Differences:  []string{"spec.replicas: 2 -> 3", "spec.template.spec.containers[0].image"},
				GitValue:     3,
				ClusterValue: 2,
			},
			{
				Kind:      "Service",
				Name:      "web",
				Namespace: "apps",
				DriftType: gitops.DriftTypeMissing,
			},
		},
	}
	server := &Server{
		restConfigFactory: func(_ string) (*rest.Config, error) {
			return &rest.Config{Host: "https://cluster.example"}, nil
		},
		manifestReaderFactory: func() manifestReader { return reader },
		driftDetectorFactory:  func(_ *rest.Config) (driftDetector, error) { return detector, nil },
	}

	result, rpcErr := callTool(t, server, "detect_drift", map[string]interface{}{
		"repo_url": "https://github.com/example/configs",
		"path":     "clusters/dev",
		"branch":   "main",
		"cluster":  "member1",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success result, got error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text

	// Header fields
	for _, want := range []string{
		"# GitOps Drift Detection",
		"**Repository:** https://github.com/example/configs",
		"**Path:** clusters/dev",
		"**Branch:** main",
		"**Cluster:** member1",
		"**Manifests Found:** 3",
		"⚠️ **Drift detected**: 2 resource(s) out of sync",
		"- Missing from cluster: 1",
		"- Modified in cluster: 1",
		"### 📝 Deployment/web", // modified → 📝
		"### ❌ Service/web",    // missing → ❌
		"**Namespace:** apps",
		"**Type:** modified",
		"**Type:** missing",
		"**Differences:**",
		"- spec.replicas: 2 -> 3",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, text)
		}
	}

	// Verify the receiving detector saw the right cluster name.
	if detector.receivedCluster != "member1" {
		t.Fatalf("detector received cluster %q, want member1", detector.receivedCluster)
	}

	// Parse the JSON payload embedded at the end.
	start := strings.Index(text, "```json\n")
	end := strings.LastIndex(text, "\n```")
	if start == -1 || end == -1 || end <= start+8 {
		t.Fatalf("expected JSON code block at end of output, got: %s", text)
	}
	var payload struct {
		Drifted   bool `json:"drifted"`
		Resources []struct {
			Kind         string   `json:"kind"`
			Name         string   `json:"name"`
			Namespace    string   `json:"namespace"`
			DriftType    string   `json:"driftType"`
			Field        string   `json:"field,omitempty"`
			Differences  []string `json:"differences,omitempty"`
			GitValue     string   `json:"gitValue,omitempty"`
			ClusterValue string   `json:"clusterValue,omitempty"`
		} `json:"resources"`
		Summary map[string]int `json:"summary"`
	}
	if err := json.Unmarshal([]byte(text[start+8:end]), &payload); err != nil {
		t.Fatalf("failed to decode JSON payload: %v\n%s", err, text[start+8:end])
	}

	if !payload.Drifted {
		t.Fatal("expected drifted=true")
	}
	if len(payload.Resources) != 2 {
		t.Fatalf("expected 2 resources in JSON, got %d", len(payload.Resources))
	}
	// First resource: modified deployment with differences
	r0 := payload.Resources[0]
	if r0.Kind != "Deployment" || r0.Name != "web" || r0.Namespace != "apps" || r0.DriftType != "modified" {
		t.Fatalf("resource[0] mismatch: %#v", r0)
	}
	if r0.Field != "spec.replicas: 2 -> 3" {
		t.Fatalf("resource[0].field = %q, want first difference", r0.Field)
	}
	if len(r0.Differences) != 2 {
		t.Fatalf("resource[0].differences len = %d, want 2", len(r0.Differences))
	}
	if r0.GitValue != "3" || r0.ClusterValue != "2" {
		t.Fatalf("resource[0] git/cluster value = %q/%q, want 3/2", r0.GitValue, r0.ClusterValue)
	}
	// Second resource: missing service with no differences → no field/git/cluster keys
	r1 := payload.Resources[1]
	if r1.Kind != "Service" || r1.Name != "web" || r1.Namespace != "apps" || r1.DriftType != "missing" {
		t.Fatalf("resource[1] mismatch: %#v", r1)
	}
	if r1.Field != "" || len(r1.Differences) != 0 || r1.GitValue != "" || r1.ClusterValue != "" {
		t.Fatalf("resource[1] should have no field/differences/values, got %#v", r1)
	}
	// Summary counts
	wantSummary := map[string]int{"total": 3, "synced": 1, "drifted": 2, "missing": 1, "modified": 1}
	for k, want := range wantSummary {
		if payload.Summary[k] != want {
			t.Fatalf("summary[%q] = %d, want %d", k, payload.Summary[k], want)
		}
	}

	if !reader.cleaned {
		t.Fatal("expected reader.Cleanup to be called via defer")
	}
}

func TestServer_NewFactories_FallbackWhenUnset(t *testing.T) {
	// When no manifestReaderFactory is set, newManifestReader returns
	// a real gitops.NewManifestReader() (non-nil). Same for
	// newDriftDetector without a factory — it delegates to
	// gitops.NewDriftDetector which requires a valid rest.Config; we
	// pass a syntactically valid Host and only require the factory
	// path itself is exercised (either non-nil detector OR non-nil
	// error, both prove the fallback branch ran).
	s := &Server{}

	r := s.newManifestReader()
	if r == nil {
		t.Fatal("newManifestReader fallback returned nil")
	}
	// Cleanup must be safe to call on the real reader.
	r.Cleanup()

	// newDriftDetector with a real config: we accept either outcome
	// (success or error) — both prove the non-factory branch ran.
	// A minimal in-cluster-like rest.Config with just Host is enough
	// for kubernetes.NewForConfig to construct clients without
	// contacting the API server.
	det, err := s.newDriftDetector(&rest.Config{Host: "https://api.example.local"})
	if err == nil && det == nil {
		t.Fatal("newDriftDetector fallback returned (nil, nil)")
	}
}

// Compile-time assertion: the fakes still satisfy the package-private
// interfaces used by tools_drift.go. If either interface changes shape,
// this line will fail to compile and flag the drift tests for review.
var (
	_ manifestReader = (*fakeManifestReader)(nil)
	_ driftDetector  = (*fakeDriftDetector)(nil)
)

var _ context.Context = context.TODO() // keep context import used
