# kubestellar-mcp Roadmap

> **Status**: Living document — updated each minor release cycle.

This roadmap describes the planned direction for `kubestellar-mcp` across three horizons.
It is not a commitment; priorities shift with community feedback and upstream changes.
File an issue or comment here to influence what gets prioritized.

## Current State — v0.8.x (May 2026)

### What works today

| Capability | Status |
|------------|--------|
| **kubestellar-ops** MCP server | ✅ Stable |
| **kubestellar-deploy** MCP server | ✅ Stable |
| Claude Code plugin (Homebrew + marketplace) | ✅ Stable |
| VS Code / Cursor / Windsurf (stdio) | ✅ Stable |
| Multi-cluster cluster listing and health | ✅ Stable |
| RBAC audit and permission analysis | ✅ Stable |
| Gatekeeper / OPA policy tools | ✅ Stable |
| App-centric deployment across clusters | ✅ Stable |
| GitOps drift detection | ✅ Stable |
| Helm install / rollback across clusters | ✅ Stable |
| Kustomize build + apply | ✅ Stable |
| Prometheus reconciliation metrics | 🔄 In progress (PR #3785 in kubestellar core) |
| OpenSSF Scorecard CI | ⚠️ Broken — stale worktree submodule (#170) |
| Dependabot / automated dep updates | ❌ Not configured (#179) |
| Branch protection on `main` | ❌ Not enforced (#178) |

---

## Near-term — v0.9.x (Q3 2026)

### Discovery and distribution
- [ ] Submit `kubestellar-ops` and `kubestellar-deploy` to [modelcontextprotocol/servers](https://github.com/modelcontextprotocol/servers)
- [ ] List on [glama.ai/mcp/servers](https://glama.ai/mcp/servers)
- [ ] Add GitHub topics: `mcp`, `model-context-protocol`, `kubernetes`, `multi-cluster`, `ai-agents`
- [ ] Link from main `kubestellar/kubestellar` README ecosystem section

### Technical debt / hardening
- [ ] Fix OpenSSF Scorecard CI — remove stale `.worktrees/docs-61-62` from git index (#170)
- [ ] Configure Dependabot for automated Go module updates (#179)
- [ ] Enforce branch protection on `main` (require PR + 1 approver + status checks) (#178)
- [ ] Harden workflow token permissions to least-privilege (`read-all` default, scoped writes) (#172)
- [ ] Pin all GitHub Actions to commit SHAs (#171)
- [ ] Validate and restrict URL schemes in GitOps manifest fetching (HTTP → HTTPS only) (#175)
- [ ] Pin GoReleaser action version (remove `version: latest`) (#176)

### Tooling expansion
- [ ] Domain-split `pkg/mcp/server/tools.go` (65KB → focused files) — architect PR #177
- [ ] Add Prometheus metrics to ops server reconciliation path
- [ ] Improve error messages for RBAC analysis on locked-down clusters

---

## Mid-term — v1.0 (Q4 2026 / Q1 2027)

### Stable API contract
- Finalize MCP tool names and parameter schemas — breaking changes only via deprecation cycle
- Publish tool schemas to docs site

### KubeStellar core integration
- Use KubeStellar `BindingPolicy` for intelligent workload placement via `kubestellar-deploy`
- Surface KubeStellar WDS/ITS/WEC topology in `kubestellar-ops` cluster listing
- Connect drift detection to KubeStellar reconciliation loop

### A2A (Agent-to-Agent) protocol
- Evaluate implementing A2A server protocol so kubestellar-mcp can participate in multi-agent orchestration pipelines
- Target: LangChain, CrewAI, AutoGen as consumer frameworks
- Related: kubestellar/kubestellar#3790

### Observability
- Structured logging for all MCP tool calls (audit trail)
- Expose Prometheus metrics from the MCP servers themselves

---

## Long-term — v1.x+ (2027+)

- **CNCF alignment**: pursue inclusion in CNCF landscape under "Multi-Cluster Management" + "AI/MLOps" categories
- **Additional AI runtimes**: Gemini, Claude API (direct), OpenAI function calling
- **Web UI**: lightweight dashboard for cluster operations (complements console project)
- **Plugin SDK**: public API for third-party tool contributions

---

## Non-goals

- **Replacing kubectl**: kubestellar-mcp is an AI-native layer on top of kubectl, not a replacement
- **Single-cluster management**: the ops/deploy binaries work on any kubeconfig, but the unique value is multi-cluster orchestration
- **Implementing KubeStellar core features**: controller logic lives in `kubestellar/kubestellar`

---

## How to Influence This Roadmap

1. **Upvote issues** using 👍 reactions
2. **Comment** on items you depend on
3. **Open a PR** — contributions accelerate everything
4. **Join the community**: [Slack #kubestellar](https://cloud-native.slack.com/archives/C097094RZ3M)

---

*Maintained by the KubeStellar MCP sub-project. Last updated: 2026-06-06.*
