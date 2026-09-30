# Integration tests

This package holds the end-to-end integration suite for kubestellar-mcp's
MCP tool surface, added to close kubestellar-mcp#1063 ("no end-to-end /
integration test suite for MCP tool surface"). Every file in this package
carries the `//go:build integration` build tag, so it is excluded from the
default `go test ./...` / `make test` fast path and from the per-package
coverage ratchet (`.github/go-package-coverage-ratchet.txt`), which only
scans `pkg/`, `cmd/`, and `internal/`.

## What it tests (Option A: controller-runtime envtest)

We use [`sigs.k8s.io/controller-runtime/pkg/envtest`](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/envtest)
to start a real `kube-apiserver` + `etcd` pair per test binary, with no
kubelet and no container runtime. That gives every MCP tool handler a real
API server round-trip (discovery, RBAC error mapping, dynamic-GVR
resolution) for the price of a local process, and it runs unmodified on
GitHub-hosted runners — no Docker-in-Docker, no `kind` cluster, no cluster
secrets.

Covered packages:

- `pkg/deploy/mcp/kubectl` — `kubectl_apply.go`
  (`kubectl_test.go`): applies a ConfigMap manifest via
  `kubectl.HandleKubectlApply`, confirms it round-trips (create then a
  second apply reports `updated`), then deletes it via
  `kubectl.HandleDeleteResource` and confirms envtest reports `not-found`
  on a second delete.
- `pkg/mcp/server/workloads` — `get_pods` (`workloads_test.go`): creates a
  Pod object directly against the envtest API server, then drives the
  `get_pods` tool handler (via `workloads.Register` +
  `handlers.Registry.Find`, exactly as the real MCP protocol server
  dispatches it) and asserts the pod shows up in the tool's output.

### Why `pkg/deploy/mcp/helm` is out of scope for envtest

`envtest` only runs `kube-apiserver` + `etcd`; there is no kubelet, no
container runtime, and no `helm` binary in the CI image by default. A real
`helm install` round-trip also depends on chart repository access (network)
or a bundled local chart plus the Helm SDK's storage driver, which layers
significant harness complexity on top of the base envtest setup for a tool
that is a thin wrapper around the `helm` CLI/SDK rather than direct
apiserver calls. Per the issue's own recommendation, this is a documented
gap rather than a forced-fit envtest test: revisit if/when a kind-based
(Option B) suite is introduced, which does have a real kubelet and can
install a chart end-to-end.

## Running locally

Prerequisites: Go (matching `go.mod`), and `setup-envtest` to fetch the
`kube-apiserver`/`etcd`/`kubectl` binaries envtest needs (no live cluster or
kubeconfig required):

```sh
go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest
export KUBEBUILDER_ASSETS="$(setup-envtest use -p path 1.30.x)"
make test-integration
```

`make test-integration` runs `go test -tags integration ./test/integration/...`.

## CI

`.github/workflows/integration-test.yml` runs this suite on pull requests
that touch `pkg/**`, `internal/**`, `test/integration/**`, `Makefile`,
`go.mod`, or `go.sum`. It installs `setup-envtest`, downloads the
`kube-apiserver`/`etcd` binaries into `KUBEBUILDER_ASSETS`, and runs
`make test-integration`.
