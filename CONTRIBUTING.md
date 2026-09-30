# Contributing to kubestellar-mcp

Thanks for contributing to kubestellar-mcp.

## Before you start

- Check the open issues and claim the one you want to work on.
- This repo has two binaries:
  - `cmd/kubestellar-ops` for diagnostics, RBAC, and security tools
  - `cmd/kubestellar-deploy` for deployment and GitOps tools
- Keep docs in sync when tool behavior changes, especially `README.md`, `docs/index.md`, `CONTRIBUTING.md`, and the matching file in `commands/`.

## Local development

### Prerequisites

- **Go 1.26+** is required to build the binaries. Check your version with `go version`. See the [Go installation guide](https://go.dev/doc/install) or the [README From Source section](README.md#from-source) for setup instructions.

### Building and Testing

- Build the target binary with `go build ./cmd/kubestellar-ops` or `go build ./cmd/kubestellar-deploy`.
- Run `go test ./...` before opening a PR.
- Use `git commit -s` so commits are DCO-signed.

### Integration tests

- The repository has a `test/integration/` package (kubestellar-mcp#1063) that exercises MCP tool handlers end-to-end against a real `kube-apiserver`, using [`sigs.k8s.io/controller-runtime/pkg/envtest`](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/envtest) to start a local `kube-apiserver` + `etcd` pair (no kubelet, no container runtime, no `kind`/Docker, no live cluster required).
- Every file in `test/integration/` carries the `//go:build integration` build tag, so it is excluded from the default `go test ./...` / `make test` fast path and from the per-package coverage ratchet (`.github/go-package-coverage-ratchet.txt`, which only scans `pkg/`, `cmd/`, `internal/`). See `test/integration/README.md` for what is covered (currently `pkg/deploy/mcp/kubectl` apply/delete and `pkg/mcp/server/workloads` get_pods) and why `pkg/deploy/mcp/helm` is a documented gap for this harness.
- Run it locally with:

  ```sh
  go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest
  export KUBEBUILDER_ASSETS="$(setup-envtest use -p path 1.30.x)"
  make test-integration
  ```

- `make test-integration` runs `go test -tags integration ./test/integration/...`. CI runs it via `.github/workflows/integration-test.yml` on pull requests touching `pkg/**`, `internal/**`, `test/integration/**`, `Makefile`, `go.mod`, or `go.sum`.
- `go test ./...` (no tags) is still worth running locally because CI runs the same command as part of `.github/workflows/build-test.yml` and it catches package-loading and compile regressions even before dedicated tests are added.
- If your change needs verification against a real cluster beyond what envtest can exercise (e.g. kubelet/Pod-lifecycle behavior, Helm chart installs), validate it manually with a working Kubernetes context before opening a PR. At minimum, make sure `kubectl get nodes` works and that `kubectl config current-context` points at the cluster you expect, and describe that manual verification in your PR.
- New integration coverage should stay in `test/integration/` (or another clearly named, build-tag-gated integration-only package) with the required tooling documented alongside it.

## Adding or changing a tool

1. Update the tool registration and handler in the relevant package under `pkg/`.
2. Add or update the command doc in `commands/`.
3. Update the README if the tool changes user-facing permissions, examples, or setup steps.
4. Keep the change focused to the tool and its docs.

### Command Documentation Format

Files in `commands/` are **Claude Code slash-command reference docs**. They document how users invoke MCP tools through the Claude Code interface.

**File naming:** Use the bare command name (e.g., `deploy.md`, `app-logs.md`).

**Required sections:**
- **Title** (`# Command Name`) — The slash-command name
- **Usage** — Brief description of what the command does
- **Examples** — Natural language requests users might type
- **What it does** — Step-by-step explanation of the command's behavior
- **MCP Tools Used** — List of MCP tools the command calls (with brief descriptions)

**Optional sections:**
- **Implementation** — Details on tool parameters or behavior
- **Smart Placement** or other feature-specific notes

**Example:** See [`commands/deploy.md`](commands/deploy.md) for a canonical reference.

## Pull requests

- Reference the related issue in the PR body.
- Prefer small, reviewable documentation or tool changes.

The project is governed by the
[KubeStellar Project Governance](https://github.com/kubestellar/kubestellar/blob/main/GOVERNANCE.md).
