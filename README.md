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

```bash
git clone https://github.com/kubestellar/kubestellar-mcp.git
cd kubestellar-mcp

# Build both binaries
go build -o bin/kubestellar-ops ./cmd/kubestellar-ops
go build -o bin/kubestellar-deploy ./cmd/kubestellar-deploy

sudo mv bin/kubestellar-* /usr/local/bin/
```

## Claude Code Plugins

### Install the Plugins

1. Add the KubeStellar marketplace:
   ```
   /plugin marketplace add kubestellar/claude-plugins
   ```
2. Go to `/plugin` → **Discover** tab
3. Install **kubestellar-ops** and/or **kubestellar-deploy**

### Verify Installation

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
| `/k8s-ownership` | Manage ownership tracking with OPA Gatekeeper |

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

| Variable | Description |
|----------|-------------|
| `KUBECONFIG` | Path to kubeconfig file |

## Related Projects

- [KubeStellar](https://kubestellar.io) - Multi-cluster orchestration platform

## Contributing

Contributions are welcome! Please read our [contributing guidelines](CONTRIBUTING.md).

## License

Apache License 2.0 - see [LICENSE](LICENSE) for details.
