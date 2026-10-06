# Alert Rules

This directory holds an importable Prometheus/PrometheusRule alert rule
file for `kubestellar-mcp`. Like `docs/dashboards/`, this is a static
artifact only — nothing in this repository applies the rule or ships data
anywhere. A maintainer who already runs Prometheus (or Prometheus Operator)
and has opted the MCP server into the metrics endpoint (`--metrics-addr`,
see `pkg/metrics`) can apply this as-is.

**Scope: `kubestellar-ops` and `kubestellar-deploy` share these metric
names and, as of `--metrics-addr` support on both binaries, can both
expose them.** `kubestellar-deploy` (`pkg/deploy/`) is a second
MCP-server binary that imports the same `pkg/metrics` package and
records into the same `mcpserver_*` series as `kubestellar-ops`, with no
binary-distinguishing label. `kubestellar-deploy` now also registers an
`--metrics-addr` flag and serves `/metrics` (see
`pkg/deploy/cmd/root.go`), so an operator who enables it on both
binaries **must** scrape them as separate Prometheus targets (distinct
`job`/`instance` labels, or a relabel step) before applying these rules
— otherwise every expression below would aggregate both services'
traffic together, and an alert firing couldn't tell you which binary is
actually degraded. See the "Scope note" in [`../slo.md`](../slo.md) for
which SLOs apply to which binary.

## `mcpserver-rules.yaml`

Alert rules aligned with the SLOs in [`../slo.md`](../slo.md):

- `MCPServerHighToolErrorRate` / `MCPServerCriticalToolErrorRate` — tool
  error rate versus the SLO 1 error budget.
- `MCPServerHighToolLatencyP95` — p95 tool latency (all tool calls) using
  SLO 2's p95 target as a reference threshold; it is a general latency
  proxy, not a direct measurement of SLO 2's discovery-latency SLI.
- `MCPServerHighAIQueryErrorRate` — AI provider query error rate versus
  the SLO 5 error budget.
- `MCPServerHighAIQueryLatencyP95` — p95 AI provider query latency versus
  SLO 5 targets.
- `MCPServerHighGitOpsSyncFailureRate` — GitOps sync resource failure rate
  (`mcpserver_gitops_sync_total{action="failed"}`) versus a general
  operational threshold; not yet tied to a formal SLO in
  [`../slo.md`](../slo.md) (`kubestellar-deploy`-only — `kubestellar-ops`
  does not call the GitOps `Syncer`).
  **No equivalent alert exists for `mcpserver_gitops_drift_total`:**
  drift detection reports informational drift counts (`missing`/
  `modified`), not pass/fail outcomes like sync, and resource-check
  failures encountered while detecting drift (API errors, RBAC denials)
  are folded into `drift_type="missing"` with no distinct error signal —
  see `pkg/gitops/drift.go`'s `checkResource` error path. Correlate with
  `gitops drift check failed` log lines, not this metric, to find actual
  check failures.
- `MCPServerHighMulticlusterOperationFailureRate` — per-cluster
  multi-cluster fan-out operation failure rate
  (`mcpserver_multicluster_operation_total{status="error"}`) versus a
  general operational threshold; not yet tied to a formal SLO in
  [`../slo.md`](../slo.md) (`kubestellar-deploy`-only — `kubestellar-ops`
  never constructs a `multicluster.Executor`, same scoping caveat as
  `MCPServerActiveClustersDroppedToZero` below).
- `MCPServerActiveClustersDroppedToZero` — reachable-cluster count drop,
  cross-referenced with the connectivity-loss runbook section.
  **`kubestellar-deploy`-only:** `mcpserver_active_clusters` is set solely
  by `multicluster.Executor.executeAll` (`pkg/multicluster/executor.go`),
  which only `kubestellar-deploy` constructs (`pkg/deploy/mcp/server.go`).
  `kubestellar-ops` never calls `metrics.SetActiveClusters`, so applying
  this rule to a `kubestellar-ops` scrape target makes it fire
  permanently (the gauge never leaves its zero default), not on an actual
  connectivity regression.
- `MCPServerScrapeTargetDown` — the `/metrics` scrape target itself is
  unreachable (standard Prometheus `up` metric), independent of the
  `mcpserver_*` series the other rules depend on. Adjust the
  `job`/`instance` selector in the expression to match your scrape
  config.

## Applying

With Prometheus Operator:

```bash
kubectl apply -f docs/alerts/mcpserver-rules.yaml
```

With plain Prometheus, add the `spec.groups` content to a file referenced
by your `rule_files` configuration (the `apiVersion`/`kind`/`metadata`
wrapper is Prometheus-Operator-specific and can be stripped).

## Validating changes

`make alert-lint` checks that the file is syntactically valid YAML and
contains the expected `spec.groups` rule structure. It does not require a
running Prometheus instance.
