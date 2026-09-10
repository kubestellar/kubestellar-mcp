# Alert Rules

This directory holds an importable Prometheus/PrometheusRule alert rule
file for `kubestellar-mcp`. Like `docs/dashboards/`, this is a static
artifact only — nothing in this repository applies the rule or ships data
anywhere. A maintainer who already runs Prometheus (or Prometheus Operator)
and has opted the MCP server into the metrics endpoint (`--metrics-addr`,
see `pkg/metrics`) can apply this as-is.

**Scope: `kubestellar-ops` and `kubestellar-deploy` share these metric
names.** `kubestellar-deploy` (`pkg/deploy/`) is a second MCP-server binary
that uses the same `pkg/metrics` package and the same `mcpserver_*` metric
names as `kubestellar-ops`, with no binary-distinguishing label. If you run
`--metrics-addr` on both binaries, scrape them as separate Prometheus
targets (distinct `job`/`instance` labels, or a relabel step) before
applying these rules — otherwise every expression below aggregates both
services' traffic together, and an alert firing can't tell you which
binary is actually degraded. See the "Scope note" in
[`../slo.md`](../slo.md) for which SLOs apply to which binary.

## `mcpserver-rules.yaml`

Alert rules aligned with the SLOs in [`../slo.md`](../slo.md):

- `MCPServerHighToolErrorRate` / `MCPServerCriticalToolErrorRate` — tool
  error rate versus the SLO 1 error budget.
- `MCPServerHighToolLatencyP95` — p95 tool latency versus SLO targets.
- `MCPServerHighAIQueryErrorRate` — AI provider query error rate versus
  the SLO 5 error budget.
- `MCPServerHighAIQueryLatencyP95` — p95 AI provider query latency versus
  SLO 5 targets.
- `MCPServerActiveClustersDroppedToZero` — reachable-cluster count drop,
  cross-referenced with the connectivity-loss runbook section.

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
