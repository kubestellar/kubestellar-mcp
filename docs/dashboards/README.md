# Dashboards

This directory holds importable Grafana dashboard JSON for `kubestellar-mcp`.
These are static artifacts only — nothing in this repository scrapes metrics
or ships data anywhere. A maintainer who already runs Prometheus + Grafana
and has opted into the MCP server's metrics endpoint (`--metrics-addr`, see
`pkg/metrics`) can import a dashboard here as-is.

## `mcpserver-overview.json`

Overview of MCP tool-dispatch traffic for the metrics defined in
`pkg/metrics/metrics.go`:

- `mcpserver_tool_calls_total` — call rate by tool and status
- `mcpserver_tool_errors_total` — error rate and breakdown by `error_kind`
- `mcpserver_tool_duration_seconds` — p50/p95/p99 latency
- `mcpserver_active_clusters` — reachable cluster count

Import via Grafana's "Import dashboard" flow (paste JSON or upload the file)
and select a Prometheus datasource that scrapes the MCP server's `/metrics`
endpoint.

## Validating changes

`make dashboard-lint` checks that every `*.json` file here is syntactically
valid JSON and contains the minimum fields (`title`, `panels`) a Grafana
dashboard needs. It does not require a running Grafana or Prometheus
instance.

**Recommendation (not added here):** wire `make dashboard-lint` into a CI
workflow triggered on changes under `docs/dashboards/**`, so future edits are
validated automatically. Adding a new workflow file requires the
`workflows` permission that this change was not granted, so it is left as a
follow-up for a maintainer.
