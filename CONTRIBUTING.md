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

- The repository does not currently have checked-in integration tests. There are no `_test.go` files, no `test/` or `integration/` directories, and no separate integration-test Make target.
- Today, `go test ./...` is still worth running because CI runs the same command and it catches package-loading and compile regressions even before dedicated tests are added.
- If your change needs verification against a real cluster, validate it manually with a working Kubernetes context before opening a PR. At minimum, make sure `kubectl get nodes` works and that `kubectl config current-context` points at the cluster you expect.
- If you add integration coverage, keep it separate from the default fast path by placing it in a dedicated location such as `test/integration/` (or another clearly named integration-only package) and document the required cluster setup and any extra tooling in the same PR.
- Integration tests in this project should assume real Kubernetes access through `kubectl`/`KUBECONFIG`, because the binaries in this repo inspect and act on live cluster state.
- CI does not currently run a dedicated integration-test job. The only automated test step in `.github/workflows/ci.yml` is `go test ./...`, so any future integration suite should be wired into CI explicitly when it is introduced.
- Until that suite exists, describe the manual cluster-backed verification you performed in your PR when a change affects behavior that only shows up against a real cluster.

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
