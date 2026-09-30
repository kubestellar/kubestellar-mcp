//go:build integration

// Package integration holds the envtest-based end-to-end suite for the MCP
// tool surface (kubestellar-mcp#1063). Every file here carries the
// `integration` build tag, so `go test ./...` (no tags) and the coverage
// ratchet never see this package; only `make test-integration` runs it. See
// README.md for what is covered, why pkg/deploy/mcp/helm is out of scope,
// and how to run the suite locally.
package integration

import (
	"fmt"
	"os"
	"testing"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// testEnv is the shared envtest environment (one kube-apiserver + etcd pair)
// for every test in this package, started once in TestMain and reused
// across test functions to keep the suite fast.
var testEnv *envtest.Environment

// testCfg is the *rest.Config for testEnv, valid only between TestMain's
// Start() and Stop() calls.
var testCfg *rest.Config

func TestMain(m *testing.M) {
	testEnv = &envtest.Environment{}

	cfg, err := testEnv.Start()
	if err != nil {
		fmt.Fprintf(os.Stderr, "envtest: failed to start test environment: %v\n", err)
		fmt.Fprintln(os.Stderr, "envtest requires KUBEBUILDER_ASSETS to point at kube-apiserver/etcd binaries;")
		fmt.Fprintln(os.Stderr, "run: go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest")
		fmt.Fprintln(os.Stderr, "     export KUBEBUILDER_ASSETS=\"$(setup-envtest use -p path 1.30.x)\"")
		os.Exit(2)
	}
	testCfg = cfg

	code := m.Run()

	if err := testEnv.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "envtest: failed to stop test environment: %v\n", err)
	}

	os.Exit(code)
}
