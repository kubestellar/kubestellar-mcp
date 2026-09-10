# Service Level Objectives — kubestellar-mcp

This document defines Service Level Indicators (SLIs) and Service Level Objectives (SLOs) for the `kubestellar-ops` MCP server.

## Service Description

`kubestellar-ops` is a Model Context Protocol (MCP) server and kubectl plugin for multi-cluster Kubernetes diagnostics, RBAC analysis, drift detection, and policy enforcement. It communicates over stdio (JSON-RPC) and has no persistent state.

**Scope note — `kubestellar-deploy`:** `kubestellar-deploy` (`pkg/deploy/cmd/root.go`) is a second, separate MCP-server binary (app-centric multi-cluster deployment/GitOps) that shares the exact same `pkg/metrics` package and opt-in `--metrics-addr` flag as `kubestellar-ops`, so it emits the identical `mcpserver_tool_calls_total` / `mcpserver_tool_errors_total` / `mcpserver_tool_duration_seconds` metric names with no binary-distinguishing label. SLO 1 (Tool Response Availability) and its error-budget policy below apply equally to `kubestellar-deploy`, since the underlying metric and alert expressions cannot tell the two binaries apart on their own. SLO 2 (Cluster Discovery Latency), SLO 3 (Process Liveness text below refers to the `kubestellar-ops` process name), and SLO 4 (Cluster-Health Accuracy, a `kubestellar-ops`-only tool) are `kubestellar-ops`-specific and are not defined for `kubestellar-deploy`. **If an operator scrapes both binaries into the same Prometheus without separating them by `job`/`instance` label (see "Alerting Guidance" below), the SLO 1 alerts blend both services' traffic and no longer reliably indicate which binary is degraded.**

## SLIs and SLOs

### SLO 1 — Tool Response Availability

**SLI:** Proportion of valid MCP tool-call requests that return a non-error response within the timeout window.

**Measurement:** Track at the MCP client layer. A request is "successful" if it receives a `result` (not `error`) response within 30 seconds.

**Objective:**

| Window | Target |
|--------|--------|
| 30-day rolling | ≥ 95% of tool-call requests succeed |
| 7-day rolling | ≥ 90% of tool-call requests succeed |

**Exclusions:** Requests that fail because the target cluster API server is itself unavailable are excluded from the error budget (those failures are attributed to the cluster, not this service).

---

### SLO 2 — Cluster Discovery Latency

**SLI:** Time from `initialize` request receipt to first successful `tools/list` response.

**Measurement:** Measured at the MCP client. Timed from connection establishment to first tool list response.

**Objective:**

| Percentile | Target |
|-----------|--------|
| p50 | ≤ 500 ms |
| p95 | ≤ 2 s |
| p99 | ≤ 5 s |

---

### SLO 3 — Process Liveness

**SLI:** The `kubestellar-ops` process exits cleanly (exit code 0) on `SIGTERM` within 5 seconds.

**Measurement:** Verified in CI via `make test` and integration test suites.

**Objective:** 100% of graceful shutdown signals result in clean process exit within 5 seconds.

---

### SLO 4 — Tool-Call Accuracy (Cluster Health)

**SLI:** Proportion of `check_cluster_health` tool calls that correctly reflect the actual cluster state (healthy ↔ unhealthy) within one reconciliation cycle.

**Measurement:** Validated by integration tests comparing tool output to direct `kubectl get nodes` responses.

**Objective:** ≥ 99% accuracy in test environments; correctness is the primary reliability concern for diagnostic tools.

---

### SLO 5 — AI Provider Query Availability

**SLI:** Proportion of AI provider query invocations (`mcpserver_ai_query_total`, recorded by `pkg/ai/claude/client.go` via `metrics.RecordAIQuery`) that complete without error.

**Measurement:** `sum(rate(mcpserver_ai_query_total{status="error"}[<window>])) / sum(rate(mcpserver_ai_query_total[<window>]))`, opt-in via `--metrics-addr` per [`docs/slo.md`](#alerting-guidance) below. Only emitted when the AI provider path is exercised; excluded from the SLO when the feature is not enabled.

**Objective:**

| Window | Target |
|--------|--------|
| 30-day rolling | ≥ 95% of AI provider query invocations succeed |
| 7-day rolling | ≥ 90% of AI provider query invocations succeed |

These targets mirror SLO 1 and are the basis for the existing `MCPServerHighAIQueryErrorRate` alert in [`docs/alerts/mcpserver-rules.yaml`](alerts/mcpserver-rules.yaml) (5% over 1h).

**Exclusions:** Same as SLO 1 — failures attributable to the underlying cluster/API server, not the AI provider integration itself, are excluded.

---

## Error Budget Policy

| SLO | 30-day budget (5% = 36 hours) |
|-----|-------------------------------|
| Tool Response Availability | 36 hours of degraded availability per 30 days |
| Cluster Discovery Latency (p95) | Up to 5% of requests may exceed 2 s |
| AI Provider Query Availability | 36 hours of degraded availability per 30 days |

When the error budget for SLO 1 drops below 50%, the team should:
1. Halt non-critical feature work.
2. Prioritize reliability improvements.
3. Conduct a postmortem if the budget is fully consumed.

---

## Alerting Guidance

Since the MCP server has no HTTP interface and no Prometheus metrics endpoint (it is a stdio tool, not a daemon), SLO compliance is assessed via:

- **MCP client-side instrumentation:** Claude Code and other MCP clients can record tool-call latency and error rates.
- **CI integration tests:** `build-test.yml` runs `go test -race ./...` (covering cluster discovery and tool accuracy paths) on every push and pull request to `main`. This is event-driven, not scheduled — there is currently no `schedule:`-triggered workflow that runs the test suite independent of a code change. If several days pass with no commits, there is no standing automated check re-validating SLO 2/SLO 4 behavior against environmental drift (e.g., Kubernetes API or dependency behavior changes) in that window.
- **Container exit code monitoring:** If run in Docker or a process supervisor, monitor for non-zero exit codes.
- **Prometheus metrics (opt-in):** when an operator starts the server with `--metrics-addr`, `pkg/metrics` exposes `mcpserver_tool_calls_total`, `mcpserver_tool_errors_total`, `mcpserver_tool_duration_seconds`, `mcpserver_active_clusters`, `mcpserver_ai_query_total`, and `mcpserver_ai_query_duration_seconds` on `/metrics`. See [`docs/dashboards/`](dashboards/README.md) for an importable Grafana dashboard and [`docs/alerts/`](alerts/README.md) for `PrometheusRule` alert rules aligned with SLO 1/2/5 above. Neither is applied automatically; both require an operator-configured Prometheus.
- **Scraping `kubestellar-deploy` alongside `kubestellar-ops`:** because both binaries share the same `mcpserver_*` metric names (see "Scope note" above), an operator who enables `--metrics-addr` on both **must** scrape them as distinct Prometheus targets — e.g. separate `job`/`instance` labels or a relabel rule — before applying the alert rules in `docs/alerts/`. Scraping both into one undifferentiated target set will blend `kubestellar-ops` diagnostic traffic with `kubestellar-deploy` GitOps/blue-green-deploy traffic in every SLO 1 alert expression, masking a real outage in one binary with healthy volume from the other.

**Recommendation (not implemented here, decision left to a maintainer):** add a lightweight `schedule:`-triggered workflow (e.g., daily) that runs the existing integration test suite against a disposable cluster (kind/k3d), independent of whether code changed, to close the gap above. This is a suggestion only — no such workflow is added by this change.

---

## Review Cadence

SLOs are reviewed quarterly or after any P1/P2 incident. Changes require approval from a project maintainer.
