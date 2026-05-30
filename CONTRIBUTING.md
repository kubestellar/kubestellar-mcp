# Contributing to kubestellar-mcp

Thanks for contributing to kubestellar-mcp.

## Before you start

- Check the open issues and claim the one you want to work on.
- This repo has two binaries:
  - `cmd/kubestellar-ops` for diagnostics, RBAC, and security tools
  - `cmd/kubestellar-deploy` for deployment and GitOps tools
- Keep docs in sync when tool behavior changes, especially `README.md`, `CONTRIBUTING.md`, and the matching file in `commands/`.

## Local development

- Build the target binary with `go build ./cmd/kubestellar-ops` or `go build ./cmd/kubestellar-deploy`.
- Run `go test ./...` before opening a PR.
- Use `git commit -s` so commits are DCO-signed.

## Adding or changing a tool

1. Update the tool registration and handler in the relevant package under `pkg/`.
2. Add or update the command doc in `commands/`.
3. Update the README if the tool changes user-facing permissions, examples, or setup steps.
4. Keep the change focused to the tool and its docs.

## Pull requests

- Reference the related issue in the PR body.
- Prefer small, reviewable documentation or tool changes.

The project is governed by the
[KubeStellar Project Governance](https://github.com/kubestellar/kubestellar/blob/main/GOVERNANCE.md).
