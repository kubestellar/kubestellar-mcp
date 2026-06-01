# kubestellar-mcp

AI-powered multi-cluster Kubernetes tools for Claude Code.

**Single-cluster UX for multi-cluster reality** - work with your **apps**, not your **clusters**.

## Components

| Binary | Description |
|--------|-------------|
| **kubestellar-ops** | Multi-cluster diagnostics, RBAC analysis, security checks |
| **kubestellar-deploy** | App-centric deployment, GitOps, smart workload placement |

## Installation

### Homebrew (Recommended)

```bash
brew tap kubestellar/tap

# Install diagnostics tools
brew install kubestellar-ops

# Install deployment tools
brew install kubestellar-deploy

# Or install both
brew install kubestellar-ops kubestellar-deploy
```

### From Releases

Download from [GitHub Releases](https://github.com/kubestellar/kubestellar-mcp/releases).

### From Source

**Prerequisites:** Go 1.26+ (`go version` to verify)

```bash
git clone https://github.com/kubestellar/kubestellar-mcp.git
cd kubestellar-mcp

# Build both binaries
go build -o bin/kubestellar-ops ./cmd/kubestellar-ops
go build -o bin/kubestellar-deploy ./cmd/kubestellar-deploy

sudo mv bin/kubestellar-* /usr/local/bin/
```

## Claude Code Plugins

### Step 1: Add the KubeStellar Marketplace

In Claude Code, run:

```
/plugin marketplace add kubestellar/claude-plugins
```

### Step 2: Install the Plugins

```
/plugin install kubestellar-ops
/plugin install kubestellar-deploy
```

Or:
1. Go to `/plugin` → **Marketplaces** tab → click **Update** on kubestellar marketplace
2. Go to `/plugin` → **Discover** tab → Install **kubestellar-ops** and/or **kubestellar-deploy**

### Step 3: Verify

Run `/mcp` in Claude Code - you should see:

```
plugin:kubestellar-ops:kubestellar-ops · ✓ connected
plugin:kubestellar-deploy:kubestellar-deploy · ✓ connected
```

### Allow Tools Without Prompts

Add to `~/.claude/settings.json`:

```json
{
  "permissions": {
    "allow": [
      "mcp__plugin_kubestellar-ops_kubestellar-ops__*",
      "mcp__plugin_kubestellar-deploy_kubestellar-deploy__*"
    ]
  }
}
```

Or run in Claude Code:

```
/allowed-tools add mcp__plugin_kubestellar-ops_kubestellar-ops__*
/allowed-tools add mcp__plugin_kubestellar-deploy_kubestellar-deploy__*
```


## Other AI Clients / MCP Clients

The MCP servers can be used with any MCP-capable AI coding tool — not just Claude Code. Both `kubestellar-ops` and `kubestellar-deploy` support the MCP stdio transport when invoked directly.

### VS Code (GitHub Copilot)

Create `.vscode/mcp.json` in your workspace root:

```json
{
  "servers": {
    "kubestellar-ops": {
      "command": "kubestellar-ops",
      "args": ["--mcp-server"]
    },
    "kubestellar-deploy": {
      "command": "kubestellar-deploy",
      "args": ["--mcp-server"]
    }
  }
}
```

### Cursor

Add to `.cursor/mcp.json` in your project root (or `~/.cursor/mcp.json` for global access):

```json
{
  "mcpServers": {
    "kubestellar-ops": {
      "command": "kubestellar-ops",
      "args": ["--mcp-server"]
    },
    "kubestellar-deploy": {
      "command": "kubestellar-deploy",
      "args": ["--mcp-server"]
    }
  }
}
```

### Windsurf

Add to `~/.codeium/windsurf/mcp_config.json`:

```json
{
  "mcpServers": {
    "kubestellar-ops": {
      "command": "kubestellar-ops",
      "args": ["--mcp-server"]
    },
    "kubestellar-deploy": {
      "command": "kubestellar-deploy",
      "args": ["--mcp-server"]
    }
  }
}
```

### Generic MCP Client (stdio)

Any MCP-capable client can connect over stdio:

```bash
kubestellar-ops --mcp-server
kubestellar-deploy --mcp-server
```

The binaries speak the [Model Context Protocol](https://modelcontextprotocol.io/) over stdin/stdout. Pass `--mcp-server` to start in MCP server mode instead of the default CLI mode.

## Kubernetes RBAC

The MCP binaries use your active kubeconfig by default. If you run them in-cluster, bind the same permissions to the pod ServiceAccount.

| Use case | Typical permissions |
|----------|---------------------|
| **kubestellar-ops** read-only | `get`, `list`, `watch` on namespaces, nodes, pods, pods/log, services, endpoints, deployments, replica sets, statefulsets, daemonsets, jobs, cronjobs, events, resourcequotas, limitranges, roles, rolebindings, clusterroles, and clusterrolebindings |
| **kubestellar-deploy** write | Everything above, plus `create`, `update`, `patch`, and `delete` on the resource types you plan to manage |

Example read-only ClusterRole:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kubestellar-mcp-readonly
rules:
  - apiGroups: [""]
    resources: ["namespaces", "nodes", "pods", "pods/log", "services", "endpoints", "events", "resourcequotas", "limitranges"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["apps", "batch"]
    resources: ["deployments", "replicasets", "statefulsets", "daemonsets", "jobs", "cronjobs"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["rbac.authorization.k8s.io"]
    resources: ["roles", "rolebindings", "clusterroles", "clusterrolebindings"]
    verbs: ["get", "list", "watch"]
```

For write workflows, add `create`, `update`, `patch`, and `delete` to the resource rules you actually need.

## Troubleshooting

### Plugin not showing
- Restart Claude Code, VS Code, Cursor, or Windsurf after installing or upgrading the binary.
- Verify the binary is on your PATH with `which kubestellar-ops`.
- If it is not found, reinstall it or move it into a directory already on your PATH.

### Permission / RBAC errors
- Run `kubectl auth can-i --list` to see what your current identity can access.
- Compare the output with the permissions described in the [Kubernetes RBAC](#kubernetes-rbac) section above.
- If needed, update your Role, ClusterRole, or binding before retrying the MCP client.

### kubeconfig problems
- Confirm the active context with `kubectl config current-context`.
- Check whether `KUBECONFIG` is set and points to the kubeconfig file you expect.
- If the wrong cluster is selected, switch contexts or update the environment variable and retry.

### Manual smoke test
- You can verify the MCP server starts without opening an AI client.
- Run the initialize request below and confirm you get a JSON-RPC response back.
```bash
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{}}}' | ./bin/kubestellar-ops --mcp-server
```

### Updating

Update the CLI tools via Homebrew:

```bash
brew update
brew upgrade kubestellar-ops kubestellar-deploy
```

Update the plugins in Claude Code:

```
/plugin update kubestellar-ops
/plugin update kubestellar-deploy
```

---

## kubestellar-ops

Multi-cluster Kubernetes diagnostics, RBAC analysis, and security checks.

### Example Usage

- "List my Kubernetes clusters"
- "Find pods with issues across all clusters"
- "Check for security misconfigurations"
- "What permissions does the admin service account have?"
- "Show me warning events in kube-system"

### Features

| Category | Tools |
|----------|-------|
| **Cluster** | `list_clusters`, `get_cluster_health`, `get_nodes`, `audit_kubeconfig` |
| **Workloads** | `get_pods`, `get_deployments`, `get_services`, `get_events`, `describe_pod`, `get_pod_logs` |
| **RBAC** | `get_roles`, `get_cluster_roles`, `get_role_bindings`, `can_i`, `analyze_subject_permissions` |
| **Diagnostics** | `find_pod_issues`, `find_deployment_issues`, `check_resource_limits`, `check_security_issues` |
| **Gatekeeper** | `check_gatekeeper`, `install_ownership_policy`, `list_ownership_violations` |
| **Upgrades** | `detect_cluster_type`, `get_cluster_version_info`, `check_helm_release_upgrades` |
| **GitOps** | `detect_drift` |

### Slash Commands

| Command | Description |
|---------|-------------|
| `/k8s-health` | Check health of all clusters |
| `/k8s-issues` | Find pod and deployment issues |
| `/k8s-security` | Check for security misconfigurations |
| `/k8s-rbac` | Analyze RBAC permissions |
| `/k8s-analyze` | Comprehensive namespace analysis |
| `/k8s-audit-kubeconfig` | Audit kubeconfig clusters and recommend cleanup |
| `/k8s-ownership` | Manage ownership tracking with OPA Gatekeeper |
| `/k8s-upgrade-check` | Check for available upgrades |
| `/k8s-upgrade` | Upgrade cluster (master and nodes) |

---

## kubestellar-deploy

App-centric multi-cluster deployment and operations.

### Example Usage

- "Where is nginx running?"
- "Get logs from my api service"
- "Deploy my ML model to clusters with GPUs"
- "Are my clusters in sync with git?"
- "Scale my app to 5 replicas across all clusters"

### Features

| Category | Tools |
|----------|-------|
| **App Discovery** | `get_app_instances`, `get_app_status`, `get_app_logs` |
| **Deployment** | `deploy_app`, `scale_app`, `patch_app` |
| **Placement** | `list_cluster_capabilities`, `find_clusters_for_workload` |
| **GitOps** | `sync_from_git`, `detect_drift`, `reconcile`, `preview_changes` |
| **Helm** | `helm_install`, `helm_uninstall`, `helm_list`, `helm_rollback` |
| **Kustomize** | `kustomize_build`, `kustomize_apply`, `kustomize_delete` |
| **Resources** | `kubectl_apply`, `delete_resource` |
| **Labels** | `add_labels`, `remove_labels` |

### Slash Commands

| Command | Description |
|---------|-------------|
| `/app-status` | Show status of an app across all clusters |
| `/app-logs` | Get aggregated logs from an app |
| `/deploy` | Deploy or update an app |
| `/gitops-sync` | Sync clusters from git |
| `/gitops-drift` | Check for drift from git |

### Example Workflows

**"Where is my app running?"**
```
nginx is running on 3 clusters:
  - prod-east: 3 replicas, healthy
  - prod-west: 3 replicas, healthy
  - staging: 1 replica, healthy
```

**"Deploy to GPU clusters"**
```
Found 2 clusters with nvidia.com/gpu
Deployed to gpu-cluster-1, gpu-cluster-2
All healthy
```

**"Check for drift"**
```
Drift detected:
  - prod-west: ConfigMap/app-config differs
  - staging: Deployment/api has extra replicas
```

**"Install nginx-ingress with Helm"**
```
Installing nginx-ingress to 3 clusters...
  - prod-east: Installed nginx-ingress v1.10.0
  - prod-west: Installed nginx-ingress v1.10.0
  - staging: Installed nginx-ingress v1.10.0
All releases healthy
```

**"Apply kustomize overlay"**
```
Building kustomize from overlays/production...
Applied to 2 clusters:
  - prod-east: 5 resources applied
  - prod-west: 5 resources applied
```

**"Delete the test deployment"**
```
Deleted deployment/test from 3 clusters:
  - prod-east: deleted
  - prod-west: deleted
  - staging: deleted
```

---

## CLI Usage

### kubestellar-ops

```bash
# Run as MCP server (for Claude Code)
kubestellar-ops --mcp-server

# List clusters
kubestellar-ops clusters list

# Check cluster health
kubestellar-ops clusters health
```

### kubestellar-deploy

```bash
# Run as MCP server (for Claude Code)
kubestellar-deploy --mcp-server
```

## Environment Variables

| Variable | Used by | Description |
|----------|---------|-------------|
| `KUBECONFIG` | `kubestellar-ops`, `kubestellar-deploy` | Path to the kubeconfig file to use instead of the default Kubernetes client lookup path |
| `ANTHROPIC_API_KEY` | `kubestellar-ops` | Required for the `query` command and natural-language cluster queries backed by Claude |

### Related runtime flags

`kubestellar-ops` also inherits the standard Kubernetes client flags from `kubectl`, so contributors can override cluster selection and request behavior at runtime without additional environment variables. Common examples include:

- `--context` to select a kubeconfig context
- `--namespace` to scope namespaced operations
- `--request-timeout` to override API request timeouts
- `--cluster`, `--user`, `--server`, `--token`, and TLS flags for advanced auth/connection overrides
- `--all-clusters`, `--target-cluster`, and `--mcp-server` for KubeStellar-specific behavior

`kubestellar-deploy` currently exposes `--mcp-server` as its runtime flag and does not require any additional environment variables beyond Kubernetes client configuration.

## Related Projects

- [KubeStellar](https://kubestellar.io) - Multi-cluster orchestration platform

## Contributing

Contributions are welcome! Please read our [contributing guidelines](CONTRIBUTING.md).

## License

Apache License 2.0 - see [LICENSE](LICENSE) for details.
