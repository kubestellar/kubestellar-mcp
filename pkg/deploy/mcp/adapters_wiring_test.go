package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"

	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/tooldef"
	"github.com/kubestellar/kubestellar-mcp/pkg/gitops"
)

type wiringDriftDetector struct{}

func (wiringDriftDetector) DetectDrift(context.Context, []gitops.Manifest, string) ([]gitops.DriftResult, error) {
	return nil, nil
}

// TestServerGitOpsAccessDelegatesToManager pins the gitops adapter wiring:
// the ClusterAccess shim must delegate to the server's multicluster manager
// rather than carry any state of its own.
func TestServerGitOpsAccessDelegatesToManager(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
		"beta":  "https://beta.example.com",
	})
	access := &serverGitOpsAccess{s: server}

	clusters, err := access.DiscoverClusters()
	require.NoError(t, err)
	names := make([]string, 0, len(clusters))
	for _, c := range clusters {
		names = append(names, c.Name)
	}
	assert.ElementsMatch(t, []string{"alpha", "beta"}, names)

	cfg, err := access.GetConfig("alpha")
	require.NoError(t, err)
	assert.Equal(t, "https://alpha.example.com", cfg.Host)

	_, err = access.GetConfig("missing")
	assert.Error(t, err)
}

// TestGitopsServerFactoriesDelegateToServer verifies the gitops.Server built
// by the adapter routes its factories through *Server, so test overrides
// (newManifestSyncer / newDriftDetector) reach the sub-package.
func TestGitopsServerFactoriesDelegateToServer(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})
	server.newDriftDetector = func(*rest.Config) (driftDetector, error) {
		return wiringDriftDetector{}, nil
	}
	syncErr := errors.New("syncer factory called")
	server.newManifestSyncer = func(*rest.Config) (manifestSyncer, error) {
		return nil, syncErr
	}

	gs := server.gitopsServer()
	require.NotNil(t, gs.Access)
	require.NotNil(t, gs.NewManifestReader)

	dd, err := gs.NewDriftDetector(&rest.Config{})
	require.NoError(t, err)
	assert.IsType(t, wiringDriftDetector{}, dd)

	_, err = gs.NewManifestSyncer(&rest.Config{})
	assert.ErrorIs(t, err, syncErr)
}

// TestToolDefsShareOneType proves every domain adapter returns the shared
// tooldef.ToolDef directly (no per-adapter conversion), that names are
// unique across domains, and that each handler is bound (non-nil).
func TestToolDefsShareOneType(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})

	domains := map[string][]tooldef.ToolDef{
		"app":       server.appToolDefs(),
		"deploy":    server.deployToolDefs(),
		"gitops":    server.gitopsToolDefs(),
		"helm":      server.helmToolDefs(),
		"kubectl":   server.kubectlToolDefs(),
		"kustomize": server.kustomizeToolDefs(),
		"labels":    server.labelToolDefs(),
	}

	seen := map[string]string{}
	total := 0
	for domain, defs := range domains {
		require.NotEmptyf(t, defs, "domain %q returned no tools", domain)
		for _, d := range defs {
			total++
			assert.NotEmptyf(t, d.Name, "domain %q has a tool with an empty name", domain)
			assert.NotNilf(t, d.Handler, "tool %q in domain %q has a nil handler", d.Name, domain)
			assert.NotNilf(t, d.InputSchema, "tool %q in domain %q has a nil input schema", d.Name, domain)
			if prev, dup := seen[d.Name]; dup {
				t.Errorf("tool %q registered by both %q and %q", d.Name, prev, domain)
			}
			seen[d.Name] = domain
		}
	}
	assert.Len(t, server.toolDefs(), total)
}
