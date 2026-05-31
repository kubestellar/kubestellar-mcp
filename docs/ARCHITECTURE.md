# Architecture and Developer Guide

## Overview

`kubestellar-mcp` ships two Go binaries that expose Kubernetes operations as MCP tools over stdio:

- `kubestellar-ops`: diagnostics, RBAC analysis, security checks, and upgrade helpers
- `kubestellar-deploy`: app-centric multi-cluster deployment, GitOps, Helm, kubectl, kustomize, and labeling workflows

Both binaries follow the same high-level pattern:

1. a Cobra root command parses flags
2. `--mcp-server` switches the process into MCP server mode
3. the MCP server reads JSON-RPC requests from stdin
4. a tool handler performs Kubernetes work through `client-go`
5. the server writes a JSON-RPC response to stdout

## Repository structure

### `cmd/`

`cmd/` contains the compiled entrypoints.

- `cmd/kubestellar-ops/main.go` starts the diagnostics binary
- `cmd/kubestellar-deploy/main.go` starts the deployment binary

The `main` packages stay intentionally thin and delegate almost immediately into `pkg/`.

### `pkg/`

`pkg/` contains the application logic.

#### Shared and diagnostics-oriented packages

- `pkg/cmd/`: Cobra command tree for `kubestellar-ops`
  - `root.go` wires global flags, natural-language query mode, and MCP mode
  - `clusters/`, `ai/`, and `upgrade/` provide subcommands
- `pkg/mcp/server/`: the `kubestellar-ops` MCP server
  - `server.go` defines MCP request/response types, the stdio loop, tool schemas, and dispatch
  - `tools.go`, `diagnostics.go`, `multicluster.go`, and `upgrades.go` implement tool behavior
- `pkg/cluster/`: kubeconfig-based cluster discovery and health checks
- `pkg/gitops/`: manifest reading, drift detection, and sync logic reused by MCP handlers
- `pkg/ai/claude/`: optional natural-language CLI query support for `kubestellar-ops query`
- `pkg/progress/`: CLI progress helpers

#### Deployment-oriented packages

- `pkg/deploy/cmd/`: Cobra root command for `kubestellar-deploy`
- `pkg/deploy/mcp/`: the `kubestellar-deploy` MCP server and its handlers
  - `server.go` owns the MCP loop, tool catalog, and dispatch
  - `tools_app.go`, `tools_deploy.go`, `tools_gitops.go`, `tools_helm.go`, `tools_kubectl.go`, `tools_kustomize.go`, and `tools_labels.go` group handlers by domain
- `pkg/multicluster/`: kubeconfig-backed client management, cluster selection, and parallel execution across clusters

### `commands/`

`commands/` contains markdown command descriptions used by the Claude Code plugin/marketplace experience. These files explain user-facing workflows such as deploy, delete, app status, and GitOps operations. They are not compiled into the Go binaries, but they should stay aligned with the MCP tools exposed by the servers.

## Running locally

### Build the binaries

```bash
go build -o ./bin/kubestellar-ops ./cmd/kubestellar-ops
go build -o ./bin/kubestellar-deploy ./cmd/kubestellar-deploy
```

### Run an MCP server directly

The servers speak newline-delimited JSON-RPC over stdio, so they can be launched directly from a terminal:

```bash
./bin/kubestellar-ops --mcp-server
./bin/kubestellar-deploy --mcp-server
```

They will wait for MCP requests on stdin and write responses to stdout.

### Connect a local build to Claude Code

The README documents the supported plugin workflow. For local development, the easiest path is:

1. build the binary into `./bin`
2. prepend that directory to your `PATH`
3. install or update the `kubestellar/claude-plugins` marketplace in Claude Code
4. install the `kubestellar-ops` and/or `kubestellar-deploy` plugins
5. run `/mcp` in Claude Code and verify the plugin connects

Example shell setup:

```bash
export PATH="$PWD/bin:$PATH"
```

Because the plugin launches the named binary from `PATH`, putting your local build first lets Claude Code exercise your in-repo changes without publishing a release.

If you want a quick manual smoke test before opening Claude Code, send an initialize request yourself:

```bash
printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' | ./bin/kubestellar-ops --mcp-server
```

## Request/response lifecycle for an MCP tool call

The two binaries implement the same lifecycle, with slightly different internal helper packages.

### 1. Process startup

- `cmd/kubestellar-ops/main.go` calls `pkg/cmd.Execute()`
- `cmd/kubestellar-deploy/main.go` calls `pkg/deploy/cmd.Execute()`
- the root Cobra command checks `--mcp-server`
- MCP mode creates a server instance (`pkg/mcp/server.NewServer` or `pkg/deploy/mcp.NewServer`)

### 2. MCP handshake

Once started, the server reads JSON-RPC messages from stdin.

- `initialize` returns protocol version, server name/version, and tool capability metadata
- `tools/list` returns the tool catalog and JSON schema for each tool
- `initialized` / `notifications/initialized` is accepted as a notification without a response

In `kubestellar-ops`, the stdio loop lives in `pkg/mcp/server/server.go` and uses `bufio.Reader.ReadBytes('\n')`.
In `kubestellar-deploy`, the loop is in `pkg/deploy/mcp/server.go` and uses a `bufio.Scanner` with a larger buffer for larger payloads.

### 3. Tool dispatch

When Claude Code invokes `tools/call`:

1. the server unmarshals the tool name and arguments
2. `handleToolsCall` / `handleToolCall` switches on the tool name
3. the selected handler validates input and performs the operation

Examples:

- ops handlers commonly call `getClientForCluster()` or `cluster.Discoverer`
- deploy handlers commonly use `multicluster.ClientManager`, `Executor`, and `Selector`
- GitOps handlers call into `pkg/gitops`

### 4. Kubernetes work

Handlers then execute the real operation:

- read-only diagnostics call Kubernetes list/get APIs
- multi-cluster operations fan out across all discovered contexts
- deploy workflows aggregate per-cluster results
- GitOps workflows load manifests, compare desired state, and optionally apply changes

### 5. Response encoding

Finally the server wraps the result into an MCP `tools/call` response and writes one JSON line to stdout.

There is one important implementation difference:

- `kubestellar-ops` handlers usually return `(string, bool)` where the string is preformatted human-readable output and the boolean marks error state
- `kubestellar-deploy` handlers usually return structured Go values that `server.go` marshals into formatted JSON text for the MCP response body

## How to add a new tool

Start by deciding which MCP server owns the capability:

- add diagnostics, RBAC, security, and upgrade tools to `pkg/mcp/server/`
- add deployment and app-operation tools to `pkg/deploy/mcp/`

### Step 1: choose the implementation file

Keep handlers grouped by domain.

- add diagnostics logic to `diagnostics.go` or a new domain-specific file in `pkg/mcp/server/`
- add deploy/GitOps/Helm/kubectl logic to the matching `pkg/deploy/mcp/tools_*.go` file

### Step 2: add the tool schema to the tool list

Expose the tool to MCP clients by adding it to the tool catalog in the relevant server file:

- `pkg/mcp/server/server.go` → `handleToolsList`
- `pkg/deploy/mcp/server.go` → `handleListTools`

At this stage define:

- the tool name
- a clear description
- the JSON input schema
- required fields

### Step 3: wire the dispatcher

Add a new `case` in the tool dispatch switch:

- `pkg/mcp/server/server.go` → `handleToolsCall`
- `pkg/deploy/mcp/server.go` → `handleToolCall`

This is what connects the public MCP tool name to your Go handler.

### Step 4: implement the handler

Implementation conventions differ slightly by binary:

#### `kubestellar-ops`

- implement a method on `*Server`
- accept `context.Context` when the tool performs Kubernetes I/O
- use `getClientForCluster`, `discoverer`, or shared helpers
- return a readable text summary and whether the call should be marked as an error

#### `kubestellar-deploy`

- implement a method on `*Server`
- unmarshal the raw arguments into a typed request struct
- use `Executor`, `Selector`, `ClientManager`, and `pkg/gitops` helpers as needed
- return a structured response object and an `error`

### Step 5: add tests

Add focused unit tests beside the implementation.

Examples already in the repo:

- `pkg/mcp/server/tools_test.go`
- `pkg/mcp/server/diagnostics_test.go`
- `pkg/deploy/mcp/tools_app_test.go`
- `pkg/deploy/mcp/tools_gitops_test.go`

Favor the existing testing style:

- exercise the handler directly
- inject fakes/stubs for cluster discovery, kube clients, or manifest readers where supported
- assert both success output and error cases

### Step 6: update user-facing docs when needed

If the tool changes the user experience, update the relevant files:

- `README.md` or `docs/` for developer/operator guidance
- matching markdown in `commands/` if the Claude Code command descriptions should surface the new workflow

## Testing approach

The repository is mostly covered by Go unit tests colocated with the implementation.

### What is tested

- Cobra command behavior in `pkg/cmd/...` and `pkg/deploy/cmd/...`
- kubeconfig and cluster discovery logic in `pkg/cluster/` and `pkg/multicluster/`
- MCP request handling and tool execution in `pkg/mcp/server/` and `pkg/deploy/mcp/`
- GitOps helpers in `pkg/gitops/`
- Claude prompt/client helpers in `pkg/ai/claude/`

### Recommended local checks

Run these before sending changes for review:

```bash
go test ./...
go vet ./...
```

The GitHub Actions workflow in `.github/workflows/build-test.yml` also runs:

- `go build -v ./...`
- `go test -v -race -coverprofile=coverage.out ./...`
- `golangci-lint`

That combination keeps command wiring, MCP protocol handling, and Kubernetes helper logic from drifting out of sync.
