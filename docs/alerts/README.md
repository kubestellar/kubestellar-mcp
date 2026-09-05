# Alert Rules

This directory holds an importable Prometheus/PrometheusRule alert rule
file for `kubestellar-mcp`. Like `docs/dashboards/`, this is a static
artifact only — nothing in this repository applies the rule or ships data
anywhere. A maintainer who already runs Prometheus (or Prometheus Operator)
and has opted the MCP server into the metrics endpoint (`--metrics-addr`,
see `pkg/metrics`) can apply this as-is.

## `mcpserver-rules.yaml`

Alert rules aligned with the SLOs in [`../slo.md`](../slo.md):

- `MCPServerHighToolErrorRate` / `MCPServerCriticalToolErrorRate` — tool
  error rate versus the SLO 1 error budget.
- `MCPServerHighToolLatencyP95` — p95 latency across all tool calls,
  using SLO 2's p95 threshold as a reference point. This is a general
  latency proxy, not a direct measurement of SLO 2's discovery/handshake
  SLI (see `docs/slo.md` Alerting Guidance).
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
