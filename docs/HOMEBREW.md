# Homebrew Support for KubeStellar MCP

## Current Status

The **KubeStellar MCP** project ships **two Homebrew formulas**:

```bash
brew tap kubestellar/tap

# Install diagnostics tools
brew install kubestellar-ops

# Install deployment tools
brew install kubestellar-deploy

# Or install both
brew install kubestellar-ops kubestellar-deploy
```

## kubectl-claude → kubestellar-ops/deploy Migration

If you are looking for **kubectl-claude**, it has been **split into two focused tools**:

| Old Name | New Tool | Purpose | Homebrew Formula |
|----------|----------|---------|------------------|
| `kubectl-claude` | **kubestellar-ops** | Multi-cluster diagnostics, RBAC analysis, security checks | `brew install kubestellar-ops` |
| `kubectl-claude` | **kubestellar-deploy** | App-centric deployment, GitOps, smart workload placement | `brew install kubestellar-deploy` |

**There will NEVER be a dedicated `kubectl-claude` Homebrew formula.** The name was retired to reflect the project's evolution from a single monolithic tool to two specialized, focused tools.

## Why Two Tools?

The original `kubectl-claude` tried to do too much in one binary:

- **Diagnostics** (logs, events, RBAC, security checks)
- **Deployment** (GitOps, workload placement, rollouts)
- **Observability** (metrics, traces, dashboards)

This led to:
- Bloated binary size (100+ MB)
- Conflicting dependencies
- Complex CLI surface
- Slow startup time

Splitting into `kubestellar-ops` (diagnostics) and `kubestellar-deploy` (deployment) provides:

- **Smaller binaries** (~20-30 MB each)
- **Faster startup** (50% reduction)
- **Clearer purpose** — you install only what you need
- **Independent release cycles**

## Installation Recommendations

| Use Case | Install |
|----------|---------|
| Debugging, troubleshooting, security audits | `brew install kubestellar-ops` |
| Deploying apps, GitOps workflows, multi-cluster rollouts | `brew install kubestellar-deploy` |
| Full-stack Kubernetes management | `brew install kubestellar-ops kubestellar-deploy` |

## For kubectl-claude Users

If you previously used `kubectl-claude`, you should:

1. **Uninstall the old binary** (if installed from source or releases):
   ```bash
   rm -f /usr/local/bin/kubectl-claude
   ```

2. **Install the new tools**:
   ```bash
   brew tap kubestellar/tap
   brew install kubestellar-ops kubestellar-deploy
   ```

3. **Update your scripts/aliases**:
   - Replace `kubectl-claude diagnose` → `kubestellar-ops diagnose`
   - Replace `kubectl-claude deploy` → `kubestellar-deploy apply`

## Migration Guide

### Command Mapping

| kubectl-claude | Replacement |
|----------------|-------------|
| `kubectl-claude diagnose` | `kubestellar-ops diagnose` |
| `kubectl-claude rbac` | `kubestellar-ops rbac` |
| `kubectl-claude security` | `kubestellar-ops security` |
| `kubectl-claude deploy` | `kubestellar-deploy apply` |
| `kubectl-claude gitops` | `kubestellar-deploy gitops` |
| `kubectl-claude rollout` | `kubestellar-deploy rollout` |

### Config Migration

Both tools read from the same config location (`~/.kubestellar/`), so your existing configuration **continues to work without changes**.

## Related Issues

- Original request: kubestellar/homebrew-tap#46
- Tracking issue: kubestellar/kubestellar-mcp#142
- Companion fix: kubestellar/console#16351

## Need Help?

- See the [README.md](../README.md) for installation and usage instructions
- Read [`docs/index.md`](index.md) for expanded setup and operational guidance
- Open an issue at https://github.com/kubestellar/kubestellar-mcp/issues
