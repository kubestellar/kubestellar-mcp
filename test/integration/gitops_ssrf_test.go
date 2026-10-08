//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"

	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/gitops"
	upstreamgitops "github.com/kubestellar/kubestellar-mcp/pkg/gitops"
	"github.com/kubestellar/kubestellar-mcp/pkg/security/netguard"
)

// newGitopsHTTPSServer mirrors newGitopsServer but allows the production
// "https" scheme instead of "file", so repo URLs are routed through the real
// SSRF guard (pkg/gitops/repourl.go's validateRepoURLWithSchemes, backed by
// pkg/security/netguard) instead of the file://-only path the rest of this
// suite uses. No network call is ever made here: the repo host below is a
// literal blocked IP address, so netguard rejects it via net.ParseIP before
// any DNS lookup or clone is attempted.
func newGitopsHTTPSServer() *gitops.Server {
	return &gitops.Server{
		Access: gitopsSingleClusterAccess{},
		NewManifestReader: func() *upstreamgitops.ManifestReader {
			return upstreamgitops.NewManifestReaderWithSchemes(map[string]bool{"https": true})
		},
		NewManifestSyncer: func(config *rest.Config) (gitops.ManifestSyncer, error) {
			return upstreamgitops.NewSyncer(config)
		},
		NewDriftDetector: func(config *rest.Config) (gitops.DriftDetector, error) {
			return upstreamgitops.NewDriftDetector(config)
		},
	}
}

// TestGitopsSyncFromGitRejectsBlockedIP closes the integration-coverage gap
// tracked by kubestellar-mcp#1193: pkg/security/netguard's SSRF blocklist
// predicate is unit-tested (90% floor in
// .github/go-package-coverage-ratchet.txt) but was never exercised through a
// real MCP tool call. This drives sync_from_git with a repo URL whose host
// is the cloud instance metadata address (169.254.169.254) and asserts the
// tool surfaces netguard.ErrBlockedIP instead of attempting a clone.
func TestGitopsSyncFromGitRejectsBlockedIP(t *testing.T) {
	ctx := context.Background()

	srv := newGitopsHTTPSServer()
	handler := srv.Tools()
	var syncFromGit func(ctx context.Context, args json.RawMessage) (interface{}, error)
	for _, td := range handler {
		if td.Name == "sync_from_git" {
			syncFromGit = td.Handler
		}
	}
	require.NotNil(t, syncFromGit, "sync_from_git tool not registered")

	args, err := json.Marshal(map[string]interface{}{
		"repo": "https://169.254.169.254/repo.git",
		"path": "manifests/",
	})
	require.NoError(t, err)

	_, err = syncFromGit(ctx, args)
	require.Error(t, err, "sync_from_git must reject a repo URL resolving to a blocked IP")
	require.True(t, errors.Is(err, netguard.ErrBlockedIP),
		"expected error to wrap netguard.ErrBlockedIP, got: %v", err)
}
