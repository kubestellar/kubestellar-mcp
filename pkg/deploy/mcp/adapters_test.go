package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// These tests cover the cross-domain wiring in the *_adapter.go files: the
// closures and small interface shims that bind each domain sub-package's
// Deps / ClusterAccess to the root Server's shared multicluster manager,
// executor, selector, and manifest factories. Domain behavior itself is
// tested in the respective sub-packages (#997).

const adapterTestClusterURL = "https://cluster-a.example.com"

func newAdapterTestServer(t *testing.T) *Server {
	t.Helper()
	return newHelmTestServer(t, map[string]string{"cluster-a": adapterTestClusterURL})
}

func TestDeployDeps_ManifestReaderAdapterParsesDocuments(t *testing.T) {
	s := newAdapterTestServer(t)
	reader := s.deployDeps().GetManifestReader()

	manifests, err := reader.ReadFromReader(strings.NewReader("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n"))
	if err != nil {
		t.Fatalf("ReadFromReader() error = %v", err)
	}
	if len(manifests) != 1 || manifests[0].Kind != "ConfigMap" {
		t.Fatalf("ReadFromReader() = %+v, want one ConfigMap", manifests)
	}
}

func TestDeployDeps_ManifestSyncerUsesServerFactory(t *testing.T) {
	s := newAdapterTestServer(t)
	want := errors.New("factory called")
	var gotHost string
	s.newManifestSyncer = func(config *rest.Config) (manifestSyncer, error) {
		gotHost = config.Host
		return nil, want
	}

	_, err := s.deployDeps().GetManifestSyncer(&rest.Config{Host: adapterTestClusterURL})
	if !errors.Is(err, want) {
		t.Fatalf("GetManifestSyncer() error = %v, want %v", err, want)
	}
	if gotHost != adapterTestClusterURL {
		t.Fatalf("factory received host %q, want %q", gotHost, adapterTestClusterURL)
	}
}

func TestGitopsServer_AccessAndFactoriesDelegateToRootServer(t *testing.T) {
	s := newAdapterTestServer(t)
	syncerErr := errors.New("syncer factory called")
	detectorErr := errors.New("detector factory called")
	s.newManifestSyncer = func(*rest.Config) (manifestSyncer, error) { return nil, syncerErr }
	s.newDriftDetector = func(*rest.Config) (driftDetector, error) { return nil, detectorErr }

	gs := s.gitopsServer()

	clusters, err := gs.Access.DiscoverClusters()
	if err != nil {
		t.Fatalf("DiscoverClusters() error = %v", err)
	}
	if len(clusters) != 1 || clusters[0].Name != "cluster-a" {
		t.Fatalf("DiscoverClusters() = %+v, want [cluster-a]", clusters)
	}

	cfg, err := gs.Access.GetConfig("cluster-a")
	if err != nil {
		t.Fatalf("GetConfig() error = %v", err)
	}
	if cfg.Host != adapterTestClusterURL {
		t.Fatalf("GetConfig().Host = %q, want %q", cfg.Host, adapterTestClusterURL)
	}
	if _, err := gs.Access.GetConfig("missing"); err == nil {
		t.Fatal("GetConfig(missing) error = nil, want error")
	}

	if _, err := gs.NewManifestSyncer(cfg); !errors.Is(err, syncerErr) {
		t.Fatalf("NewManifestSyncer() error = %v, want %v", err, syncerErr)
	}
	if _, err := gs.NewDriftDetector(cfg); !errors.Is(err, detectorErr) {
		t.Fatalf("NewDriftDetector() error = %v, want %v", err, detectorErr)
	}
	if gs.NewManifestReader() == nil {
		t.Fatal("NewManifestReader() = nil")
	}
}

func TestGetDriftDetector_NilFactoryFallsBackToGitops(t *testing.T) {
	s := newAdapterTestServer(t)
	s.newDriftDetector = nil

	// gitops.NewDriftDetector defers all network I/O to the dynamic client,
	// so a non-routable host is safe here.
	detector, err := s.getDriftDetector(&rest.Config{Host: "https://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("getDriftDetector() error = %v", err)
	}
	if detector == nil {
		t.Fatal("getDriftDetector() = nil")
	}
}

func TestServerLabelsDeps_DelegatesToManagerExecutorAndGuards(t *testing.T) {
	s := newAdapterTestServer(t)
	deps := &serverLabelsDeps{s: s}

	names, err := deps.DiscoverClusterNames()
	if err != nil {
		t.Fatalf("DiscoverClusterNames() error = %v", err)
	}
	if len(names) != 1 || names[0] != "cluster-a" {
		t.Fatalf("DiscoverClusterNames() = %v, want [cluster-a]", names)
	}

	var seen []string
	results, err := deps.ExecuteOnSelected(context.Background(), []string{"cluster-a"}, func(_ context.Context, _ *kubernetes.Clientset, clusterName string) (interface{}, error) {
		seen = append(seen, clusterName)
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("ExecuteOnSelected() error = %v", err)
	}
	if len(results) != 1 || len(seen) != 1 || seen[0] != "cluster-a" {
		t.Fatalf("ExecuteOnSelected() results = %+v, seen = %v; want one call for cluster-a", results, seen)
	}

	if !deps.IsSensitiveKind("Secret") || deps.IsSensitiveKind("ConfigMap") {
		t.Fatal("IsSensitiveKind() does not match the root manifest_util blocklist")
	}
	if err := deps.SensitiveKindError("Secret"); err == nil || !strings.Contains(err.Error(), "Secret") {
		t.Fatalf("SensitiveKindError() = %v, want error naming the kind", err)
	}
}

func TestServerLabelsDeps_DiscoverClusterNamesPropagatesManagerError(t *testing.T) {
	// A manager built from a kubeconfig with no contexts yields an error (or
	// zero clusters) from DiscoverClusters; either way the shim must not
	// fabricate names.
	s := newHelmTestServer(t, map[string]string{})
	names, err := (&serverLabelsDeps{s: s}).DiscoverClusterNames()
	if err == nil && len(names) != 0 {
		t.Fatalf("DiscoverClusterNames() = %v, want none", names)
	}
}

// TestAdapters_SensitiveKindGuardsAreWiredFromRoot proves that the domain
// handlers reachable through the root registry are bound to the real
// manifest_util.go guards (not stand-ins): a blocked kind must be rejected
// with sensitiveKindError's exact text whether it arrives via kubectl_apply,
// delete_resource, deploy_app, or a kustomize build piped to apply/delete.
func TestAdapters_SensitiveKindGuardsAreWiredFromRoot(t *testing.T) {
	setupFakeKustomize(t)
	kustomizeDir := createTestKustomization(t, "kustomization.yaml")
	t.Setenv("FAKE_KUSTOMIZE_BUILD_STDOUT", "apiVersion: v1\nkind: Secret\nmetadata:\n  name: hostile\n")

	s := newAdapterTestServer(t)
	secretManifest := "apiVersion: v1\nkind: Secret\nmetadata:\n  name: hostile\n  namespace: default\n"
	wantSecret := sensitiveKindError("Secret").Error()

	cases := []struct {
		tool string
		args map[string]interface{}
		want string
	}{
		{tool: "kubectl_apply", args: map[string]interface{}{"manifest": secretManifest, "clusters": []string{"cluster-a"}, "dry_run": true}, want: wantSecret},
		{tool: "delete_resource", args: map[string]interface{}{"kind": "ClusterRoleBinding", "name": "hostile", "clusters": []string{"cluster-a"}, "dry_run": true}, want: sensitiveKindError("ClusterRoleBinding").Error()},
		{tool: "deploy_app", args: map[string]interface{}{"manifest": secretManifest, "clusters": []string{"cluster-a"}, "dry_run": true}, want: wantSecret},
		{tool: "kustomize_apply", args: map[string]interface{}{"path": kustomizeDir, "clusters": []string{"cluster-a"}, "dry_run": true}, want: wantSecret},
		{tool: "kustomize_delete", args: map[string]interface{}{"path": kustomizeDir, "clusters": []string{"cluster-a"}, "dry_run": true}, want: wantSecret},
	}

	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			def, ok := s.findToolDef(tc.tool)
			if !ok {
				t.Fatalf("tool %q not registered", tc.tool)
			}
			_, err := def.Handler(context.Background(), mustMarshalJSON(t, tc.args))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%s error = %v, want substring %q", tc.tool, err, tc.want)
			}
		})
	}
}
