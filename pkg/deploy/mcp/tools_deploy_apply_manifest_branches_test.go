package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/gitops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// Covers the last uncovered branches inside Server.applyManifest
// (pkg/deploy/mcp/tools_deploy.go). Coverage report before this file:
//   - line 193 : `if rawObj == nil { continue }` — dry-run skip of empty
//     documents between `---` separators.
//   - lines 207-211 : ValidateNamespace error branch INSIDE applyManifest's
//     dry-run loop. handleDeployApp pre-validates via validateManifestDocs
//     and never reaches this arm, so direct invocation is required.
//   - lines 228-229 : ReadFromReader decode error in the non-dry-run path.
//   - lines 233-234 : s.manager.GetConfig error (unknown cluster).
//   - lines 238-239 : s.getManifestSyncer factory error.
//   - lines 243-244 : syncer.Sync error propagation.
//
// These are all failure/edge branches — regressing them would silently
// swallow validation errors or lose diagnostic detail on the primary
// multi-cluster apply path.

// erroringManifestSyncer returns a caller-supplied error from Sync.
type erroringManifestSyncer struct{ err error }

func (e *erroringManifestSyncer) Sync(_ context.Context, _ []gitops.Manifest, _ string, _ gitops.SyncOptions) (*gitops.SyncSummary, error) {
	return nil, e.err
}

func TestApplyManifestDryRunSkipsNilDocuments(t *testing.T) {
	// A YAML stream with an empty document (---\n---) — the decoder yields
	// a nil rawObj and the loop must `continue` past it rather than
	// returning "would-apply" for an empty entry.
	server := newHelmTestServer(t, map[string]string{})
	manifest := "---\n" +
		"apiVersion: v1\n" +
		"kind: ConfigMap\n" +
		"metadata:\n  name: demo\n---\n" +
		"---\n" // trailing empty doc

	results, err := server.applyManifest(context.Background(), nil, "alpha", manifest, true)
	require.NoError(t, err)
	require.Len(t, results, 1, "empty documents must be skipped, not emitted as results")
	assert.Equal(t, "would-apply", results[0].Status)
	assert.Contains(t, results[0].Resource, "ConfigMap/demo")
}

func TestApplyManifestDryRunReportsInvalidNamespace(t *testing.T) {
	// System namespaces MUST be rejected. When applyManifest is invoked
	// directly (bypassing handleDeployApp's pre-check), the dry-run loop's
	// own ValidateNamespace call at tools_deploy.go:206-211 has to catch it
	// and return a single failed DeployResult with nil error (advisory,
	// not fatal).
	server := newHelmTestServer(t, map[string]string{})
	manifest := "apiVersion: v1\n" +
		"kind: ConfigMap\n" +
		"metadata:\n  name: demo\n  namespace: kube-system\n"

	results, err := server.applyManifest(context.Background(), nil, "alpha", manifest, true)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "failed", results[0].Status)
	assert.Contains(t, results[0].Message, "invalid namespace in manifest")
	assert.Equal(t, "alpha", results[0].Cluster)
}

func TestApplyManifestNonDryRunReturnsReaderDecodeError(t *testing.T) {
	// Malformed YAML on the non-dry-run path must surface as a
	// "failed to decode manifest" error from ReadFromReader.
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})

	// A bare '[' is invalid YAML at the object level and produces a
	// deterministic decoder error.
	_, err := server.applyManifest(context.Background(), nil, "alpha", "[", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode manifest")
}

func TestApplyManifestNonDryRunReturnsGetConfigError(t *testing.T) {
	// An unknown cluster name must produce a wrapped
	// "failed to get config for cluster ..." error out of applyManifest's
	// non-dry-run path.
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})
	manifest := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n"

	_, err := server.applyManifest(context.Background(), nil, "does-not-exist", manifest, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get config for cluster does-not-exist")
}

func TestApplyManifestNonDryRunReturnsSyncerFactoryError(t *testing.T) {
	// If the newManifestSyncer factory itself errors, applyManifest must
	// wrap it as "failed to create manifest syncer" — otherwise a bad
	// factory would look like a Sync failure to the caller.
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})
	factoryErr := errors.New("factory boom")
	server.newManifestSyncer = func(*rest.Config) (manifestSyncer, error) {
		return nil, factoryErr
	}
	manifest := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n"

	_, err := server.applyManifest(context.Background(), nil, "alpha", manifest, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create manifest syncer")
	assert.ErrorIs(t, err, factoryErr)
}

func TestApplyManifestNonDryRunReturnsSyncError(t *testing.T) {
	// When Sync itself errors, applyManifest must wrap it as
	// "failed to apply manifest" so multi-cluster callers can distinguish
	// it from config/factory setup errors.
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})
	syncErr := errors.New("sync boom")
	server.newManifestSyncer = func(*rest.Config) (manifestSyncer, error) {
		return &erroringManifestSyncer{err: syncErr}, nil
	}
	manifest := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n"

	_, err := server.applyManifest(context.Background(), nil, "alpha", manifest, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to apply manifest")
	assert.ErrorIs(t, err, syncErr)
}
