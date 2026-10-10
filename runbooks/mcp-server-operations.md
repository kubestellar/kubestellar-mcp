# MCP Server Operations Runbook

**Service:** `kubestellar-ops --mcp-server` (kubestellar-mcp)  
**Transport:** stdio (stdin/stdout JSON-RPC)  
**Container image:** `ghcr.io/kubestellar/kubestellar-mcp`

---

## Table of Contents

1. [Overview](#overview)
2. [Starting and Stopping](#starting-and-stopping)
3. [Cluster Discovery Failures](#cluster-discovery-failures)
4. [Credential Rotation](#credential-rotation)
5. [Multi-Cluster Connectivity Loss](#multi-cluster-connectivity-loss)
6. [Container Health Verification](#container-health-verification)
7. [Diagnosing Silent Failures](#diagnosing-silent-failures)
8. [Using the Metrics Endpoint](#using-the-metrics-endpoint)
9. [Diagnosing a Scrape Target Outage](#diagnosing-a-scrape-target-outage)
10. [Diagnosing High Tool Error Rate or Latency](#diagnosing-high-tool-error-rate-or-latency)
11. [Diagnosing High AI Provider Query Error Rate or Latency](#diagnosing-high-ai-provider-query-error-rate-or-latency)
12. [Diagnosing High GitOps Sync or Drift-Detection Latency](#diagnosing-high-gitops-sync-or-drift-detection-latency)
13. [Diagnosing High GitOps Sync Failure Rate or Multi-Cluster Fan-Out Failures/Latency](#diagnosing-high-gitops-sync-failure-rate-or-multi-cluster-fan-out-failureslatency)
14. [Diagnosing Blocked-IP (SSRF Guard) Attempts](#diagnosing-blocked-ip-ssrf-guard-attempts)
15. [Detecting a Failed Scheduled Workflow (Security Scans, Stale Triage, Release, Build/Test)](#detecting-a-failed-scheduled-workflow-security-scans-stale-triage-release-buildtest)
16. [Detecting a Broken PR-Gating Check (pull_request_target startup_failure)](#detecting-a-broken-pr-gating-check-pull_request_target-startup_failure)
17. [Escalation](#escalation)
18. [Release Rollback](release-rollback.md) (separate runbook, for a bad automated nightly/weekly release)

---

## Overview

`kubestellar-ops` runs as a Model Context Protocol (MCP) server over stdio. It receives JSON-RPC requests on stdin and writes responses to stdout. It discovers Kubernetes clusters from the kubeconfig file or environment, and routes diagnostic, RBAC, drift, policy, and workload queries to the appropriate cluster API servers.

**Key binaries:**
- `kubestellar-ops` — MCP server and CLI diagnostics tool
- `kubestellar-deploy` — GitOps deployment tool

**Kubeconfig location:** `$KUBECONFIG` or `~/.kube/config` (default)

---

## Starting and Stopping

### Start (Docker)

```bash
docker run --rm -i \
  -v "$HOME/.kube:/home/nonroot/.kube:ro" \
  ghcr.io/kubestellar/kubestellar-mcp:latest
```

### Start (binary)

```bash
kubestellar-ops --mcp-server
```

### Start with explicit kubeconfig

```bash
kubestellar-ops --mcp-server --kubeconfig /path/to/kubeconfig
```

### Stop

Send `SIGTERM` or `SIGINT` to the process. The server performs a graceful shutdown:

```bash
kill -TERM <pid>
```

In Docker:

```bash
docker stop <container_id>
```

---

## Cluster Discovery Failures

**Symptom:** MCP tools return "no clusters found" or "failed to discover clusters".

### Diagnosis steps

1. Verify the kubeconfig is mounted/accessible:
   ```bash
   kubestellar-ops clusters list
   ```

2. Check for expired credentials:
   ```bash
   kubectl --context <context-name> get nodes
   ```
   If this returns an auth error, credentials need rotation (see [Credential Rotation](#credential-rotation)).

3. Check cluster reachability:
   ```bash
   kubestellar-ops clusters health --all-clusters
   ```

4. Inspect the kubeconfig for correct context names:
   ```bash
   kubectl config get-contexts
   ```

### Recovery

- If credentials are expired: follow [Credential Rotation](#credential-rotation).
- If a cluster API server is unreachable: coordinate with the cluster owner; the MCP server will continue serving other clusters.
- If kubeconfig is malformed: replace with a valid kubeconfig and restart the server.

---

## Credential Rotation

**When:** Kubeconfig credentials expire or are revoked.

### Steps

1. Obtain new credentials from your cluster provider or identity system.

2. Update the kubeconfig:
   ```bash
   # Example: renew a kubeadm token
   kubeadm token create --print-join-command

   # Example: refresh a cloud provider credential
   aws eks update-kubeconfig --name <cluster-name> --region <region>
   gcloud container clusters get-credentials <cluster-name> --region <region>
   az aks get-credentials --resource-group <rg> --name <cluster-name>
   ```

3. Verify the new credentials:
   ```bash
   kubectl --context <context-name> get nodes
   ```

4. Restart the MCP server to pick up the updated kubeconfig:
   ```bash
   # Docker
   docker stop <container_id>
   docker run --rm -i -v "$HOME/.kube:/home/nonroot/.kube:ro" ghcr.io/kubestellar/kubestellar-mcp:latest

   # Binary
   kill -TERM <pid>
   kubestellar-ops --mcp-server &
   ```

---

## Multi-Cluster Connectivity Loss

**Symptom:** Some clusters are unreachable; MCP tools return errors for specific clusters.

> **Note:** `mcpserver_active_clusters` / `MCPServerActiveClustersDroppedToZero` only reflects `kubestellar-deploy`'s reachable-cluster count (see [`docs/slo.md`](../docs/slo.md) and [`docs/alerts/README.md`](../docs/alerts/README.md)). For `kubestellar-ops`, use the CLI checks below instead — the metric is never populated for that binary.

### Diagnosis

```bash
# Check health of all clusters
kubestellar-ops clusters health --all-clusters

# Check a specific cluster
kubestellar-ops clusters health --context <context-name>
```

### Recovery options

| Scenario | Action |
|----------|--------|
| Single cluster API server down | Wait for recovery; MCP continues serving other clusters |
| Network partition | Restore network path between MCP host and cluster API server |
| Credentials expired for one cluster | Rotate that cluster's credentials (see above) |
| Kubeconfig context removed | Re-add context to kubeconfig and restart server |

The MCP server is designed to continue serving requests for healthy clusters when some clusters are unavailable. Errors are scoped per-cluster in tool responses.

---

## Container Health Verification

The container runs as a non-root user (`nonroot:65532`). The MCP server uses stdio transport by default, with no HTTP endpoint to probe unless an operator has explicitly started it with `--metrics-addr` (see `docs/slo.md`). Use the following to verify the container is alive and responsive:

### Check the process is running

```bash
docker inspect <container_id> --format '{{.State.Status}}'
# Expected: running

docker exec <container_id> ps aux | grep kubestellar-ops
```

### Check the exit code after unexpected termination

```bash
docker inspect <container_id> --format '{{.State.ExitCode}}'
```

### Verify stdin/stdout connectivity

If integrating with an MCP client (e.g., Claude Code), check that the client reports the server as connected. A connected server responds to `initialize` requests within 5 seconds under normal load.

### Optional HTTP liveness check (when `--metrics-addr` is set)

If the server was started with `--metrics-addr`, it also serves a lightweight `/healthz` liveness endpoint alongside `/metrics`:

```bash
curl -sf http://<metrics-addr>/healthz
# Expected: HTTP 200, body "ok"
```

`/healthz` only confirms the HTTP listener/process is alive (no clusters or downstream dependencies are checked) — it is not a substitute for the tool-level diagnostics above. When `--metrics-addr` is not set, this endpoint does not exist and the process/exit-code checks above are the only options.

---

## Diagnosing Silent Failures

**Symptom:** Server process is running but tools return no output or unexpected errors.

### Steps

1. Check for panics in container logs:
   ```bash
   docker logs <container_id> 2>&1 | grep -i "panic\|fatal\|error"
   ```

2. Run a direct diagnostic query (binary mode):
   ```bash
   kubestellar-ops clusters list
   kubestellar-ops clusters health --all-clusters
   ```

3. Check Go runtime environment:
   ```bash
   # Ensure GOMAXPROCS is not set to 0
   docker exec <container_id> env | grep GOMAXPROCS
   ```

4. Check kubeconfig permissions inside the container:
   ```bash
   docker exec <container_id> ls -la /home/nonroot/.kube/config
   ```

5. If the server hangs on requests: restart it. The MCP server is stateless between requests; restarts are safe.

6. If `--metrics-addr` was set for this run, check `mcpserver_tool_errors_total` and `mcpserver_tool_calls_total` (see [Using the Metrics Endpoint](#using-the-metrics-endpoint) below) to see whether failures are concentrated on a specific tool, cluster, or `error_kind` before digging into logs further.

7. Each `"tool call succeeded"`/`"tool call failed"` structured log line from `InstrumentToolCall` (`pkg/mcp/rpcloop/instrument.go`) carries `trace_id`/`span_id` fields alongside `tool`, `cluster`, and `duration`. If an operator has wired a real `TracerProvider` (this package uses OpenTelemetry's default no-op provider otherwise, in which case both IDs log as the fixed all-zero hex string and carry no correlation value), use these IDs to pivot from a failing log line to its matching exported span/trace for request-scoped context before falling back to the tool/cluster/error_kind breakdown in step 6.

---

## Using the Metrics Endpoint

**Availability:** The `/metrics` endpoint is opt-in. It is only served when the operator passes `--metrics-addr <host:port>` at startup; by default no listener is started and no metrics are exposed (see `pkg/metrics`).

### Enabling for a diagnostic session

```bash
kubestellar-ops --mcp-server --metrics-addr 127.0.0.1:9090
curl -s http://127.0.0.1:9090/metrics | grep mcpserver_
```

**`kubestellar-deploy` now also supports this flag.**
`kubestellar-deploy` (`pkg/deploy/cmd/root.go`) imports the same
`pkg/metrics` package and records tool calls into the identical
`mcpserver_*` series as `kubestellar-ops`, and now registers its own
`--metrics-addr` flag and `/metrics` HTTP endpoint:

```bash
kubestellar-deploy --mcp-server --metrics-addr 127.0.0.1:9091
curl -s http://127.0.0.1:9091/metrics | grep mcpserver_
```

**Shared-registry caveat:** because both binaries emit the same metric
names with no binary-distinguishing label, running both with `--metrics-addr`
and scraping them into one Prometheus **requires** separating them by
`job`/`instance` label (or a relabel step) — otherwise the alert rules in
[`docs/alerts/mcpserver-rules.yaml`](../docs/alerts/mcpserver-rules.yaml)
and the SLOs in [`docs/slo.md`](../docs/slo.md) would blend `kubestellar-ops`
diagnostic traffic with `kubestellar-deploy` GitOps/blue-green-deploy
traffic, masking a real outage in one binary with healthy volume from the
other. See the "Scope note" in `docs/slo.md` for which SLOs are shared and
which are `kubestellar-ops`-specific.

### What to look for

- `mcpserver_tool_calls_total{tool,cluster,status}` — call volume and success/error split per tool and cluster.
- `mcpserver_tool_errors_total{tool,cluster,error_kind}` — error volume by tool, cluster, and a closed `error_kind` enum.
- `mcpserver_tool_duration_seconds{tool,cluster}` — latency histogram; compare against [SLO 1/2](../docs/slo.md) targets.
- `mcpserver_active_clusters` — reachable cluster count from the most recent discovery; a sudden drop indicates connectivity loss (see [Multi-Cluster Connectivity Loss](#multi-cluster-connectivity-loss)). **`kubestellar-deploy`-only:** this gauge is set solely by `multicluster.Executor.executeAll` (`pkg/multicluster/executor.go`), which only `kubestellar-deploy` constructs. `kubestellar-ops` never calls `metrics.SetActiveClusters`, so on a `kubestellar-ops` target it never leaves 0 — do not use it or `MCPServerActiveClustersDroppedToZero` to monitor a `kubestellar-ops` deployment (see [`docs/slo.md`](../docs/slo.md)).
- `mcpserver_ai_query_total{provider,status}` / `mcpserver_ai_query_duration_seconds{provider}` — AI provider query volume, outcome, and latency (see `pkg/ai/claude/client.go`); watch alongside `MCPServerHighAIQueryErrorRate` in [`docs/alerts/mcpserver-rules.yaml`](../docs/alerts/mcpserver-rules.yaml).
- `mcpserver_ai_query_errors_total{provider,error_kind}` — AI provider query error volume, classified by the same closed `error_kind` enum as `mcpserver_tool_errors_total`, plus an AI-specific `ai_api` kind for non-2xx provider responses (see `pkg/ai/claude/client.go`); use to tell a local/timeout failure apart from a provider-side rejection before escalating.
- `mcpserver_gitops_sync_total{cluster,action}` / `mcpserver_gitops_sync_duration_seconds{cluster}` — GitOps sync resource outcomes (`created`/`updated`/`unchanged`/`failed`/`skipped`) and latency per cluster (see `pkg/gitops/sync.go`); watch alongside `MCPServerHighGitOpsSyncFailureRate` and `MCPServerHighGitOpsSyncLatencyP95` in [`docs/alerts/mcpserver-rules.yaml`](../docs/alerts/mcpserver-rules.yaml) — see [Diagnosing High GitOps Sync or Drift-Detection Latency](#diagnosing-high-gitops-sync-or-drift-detection-latency) if the latter fires.
- `mcpserver_gitops_drift_total{cluster,drift_type}` / `mcpserver_gitops_drift_duration_seconds{cluster}` — detected drift count (`missing`/`modified`) and detection latency per cluster (see `pkg/gitops/drift.go`). **Caveat:** unlike `mcpserver_gitops_sync_total`, there is no count/rate `MCPServer*` alert for `mcpserver_gitops_drift_total` itself (though `MCPServerHighGitOpsDriftLatencyP95` does cover its latency — see [Diagnosing High GitOps Sync or Drift-Detection Latency](#diagnosing-high-gitops-sync-or-drift-detection-latency)), and resource-check errors during drift detection (API errors, RBAC denials in `checkResource`) are recorded as `drift_type="missing"` with no distinct label — do not treat a `missing` count spike as confirmed drift without also checking for `gitops drift check failed` log lines (see [`docs/alerts/README.md`](../docs/alerts/README.md)).

### Dashboard

A ready-to-import Grafana dashboard for these metrics is at
[`docs/dashboards/mcpserver-overview.json`](../docs/dashboards/mcpserver-overview.json)
(see [`docs/dashboards/README.md`](../docs/dashboards/README.md)). It requires a
Prometheus instance already scraping this server's `/metrics` endpoint — no
scrape config or backend is bundled with this repository.

---

## Diagnosing a Scrape Target Outage

**Symptom:** The `MCPServerScrapeTargetDown` alert in
[`docs/alerts/mcpserver-rules.yaml`](../docs/alerts/mcpserver-rules.yaml) has
fired.

This is distinct from every other alert in that file: it fires on the
standard Prometheus `up` metric for the scrape target itself, not on any
`mcpserver_*` series. Once the `/metrics` endpoint is unreachable, the
`mcpserver_*` series it would normally emit go stale and drop out of
instant-vector queries, so the ratio/gauge-based alerts above cannot fire —
this is the only alert that still pages when the process is fully down or
unreachable.

### Steps

1. Confirm the process/container state first, since this alert means the
   scrape itself is failing, not that error rates are elevated:
   ```bash
   docker inspect <container_id> --format '{{.State.Status}}'
   docker inspect <container_id> --format '{{.State.ExitCode}}'
   ```
   See [Container Health Verification](#container-health-verification) for
   the full sequence.

2. If the container is running, check that `--metrics-addr` is still the
   flag the process was started with, and that the listener is reachable
   from the Prometheus scrape target (network policy, port mapping,
   firewall):
   ```bash
   docker exec <container_id> curl -s http://127.0.0.1:<port>/metrics | head
   ```

3. Check container logs for a panic or fatal error around the time the
   scrape started failing (see [Diagnosing Silent
   Failures](#diagnosing-silent-failures)).

4. If the process is gone or hung, restart it — the MCP server is
   stateless between requests, so restarts are safe (see [Starting and
   Stopping](#starting-and-stopping)).

5. Once the endpoint is reachable again, confirm the alert clears and check
   whether any of the ratio/gauge alerts above (`MCPServerHighToolErrorRate`,
   `MCPServerActiveClustersDroppedToZero`, etc.) also fire once fresh data
   arrives — the outage window may have hidden a real error-rate or
   connectivity regression.

---

## Diagnosing High Tool Error Rate or Latency

**Symptom:** The `MCPServerHighToolErrorRate` or `MCPServerHighToolLatencyP95`
alert in [`docs/alerts/mcpserver-rules.yaml`](../docs/alerts/mcpserver-rules.yaml)
has fired (see [SLO 1/2](../docs/slo.md)).

### Steps

1. Enable the metrics endpoint if it is not already running for this
   deployment (see [Using the Metrics Endpoint](#using-the-metrics-endpoint)
   above).

2. Isolate the affected tool and cluster:
   ```bash
   curl -s http://127.0.0.1:9090/metrics | grep 'mcpserver_tool_errors_total\|mcpserver_tool_duration_seconds'
   ```
   Compare `mcpserver_tool_errors_total{tool,cluster,error_kind}` and
   `mcpserver_tool_duration_seconds{tool,cluster}` across tools/clusters —
   a spike concentrated on one `cluster` label usually points to that
   cluster's API server rather than the MCP server itself.

3. If errors/latency are concentrated on one cluster:
   ```bash
   kubectl --context <context-name> get --raw='/readyz?verbose'
   ```
   A slow or degraded cluster API server is excluded from the SLO 1 error
   budget (see [SLO 1 exclusions](../docs/slo.md#slis-and-slos)), but still
   merits following up with that cluster's owner.

4. If errors/latency span multiple clusters and tools: check for a recent
   `kubestellar-ops` binary/image upgrade, and follow
   [Diagnosing Silent Failures](#diagnosing-silent-failures) for panic/log
   inspection.

5. If `error_kind` shows a concentration of `timeout`: confirm the target
   cluster is reachable at all per
   [Multi-Cluster Connectivity Loss](#multi-cluster-connectivity-loss).

---

## Diagnosing High AI Provider Query Error Rate or Latency

**Symptom:** The `MCPServerHighAIQueryErrorRate` or
`MCPServerHighAIQueryLatencyP95` alert in
[`docs/alerts/mcpserver-rules.yaml`](../docs/alerts/mcpserver-rules.yaml)
has fired (see [SLO 5](../docs/slo.md)). Note that these two alerts are
independent signals: a degraded AI provider endpoint can return slow
*successful* responses (latency alert only, no error-rate signal) or fail
outright (error-rate alert), so check both.

### Steps

1. Enable the metrics endpoint if it is not already running for this
   deployment (see [Using the Metrics Endpoint](#using-the-metrics-endpoint)
   above).

2. Isolate the affected provider:
   ```bash
   curl -s http://127.0.0.1:9090/metrics | grep 'mcpserver_ai_query_total\|mcpserver_ai_query_duration_seconds\|mcpserver_ai_query_errors_total'
   ```
   Compare `mcpserver_ai_query_total{provider,status}` and
   `mcpserver_ai_query_duration_seconds{provider}` across providers — a
   spike concentrated on one `provider` label points to that provider's
   endpoint rather than the MCP server itself. Check
   `mcpserver_ai_query_errors_total{provider,error_kind}` to tell a
   provider-side rejection (`error_kind="ai_api"`, e.g. rate limiting or
   auth failure) apart from a local `timeout` or `marshal` failure before
   escalating to the provider's status page.

3. If latency is elevated but errors are not (latency alert only): treat
   this as a possible upstream AI provider degradation. Check the
   provider's own status page/dashboard before assuming a local issue.

4. If errors are elevated: check `pkg/ai/claude/client.go` request
   handling and recent provider API changes (auth, rate limiting, schema).
   Cross-reference with [Diagnosing Silent Failures](#diagnosing-silent-failures)
   for panic/log inspection if errors span all providers.

5. Per [SLO 5 exclusions](../docs/slo.md#slo-5--ai-provider-query-availability),
   failures attributable to the underlying cluster/API server rather than
   the AI provider integration itself are excluded from this SLO, but still
   merit follow-up via [Multi-Cluster Connectivity Loss](#multi-cluster-connectivity-loss)
   if relevant.

---

## Diagnosing High GitOps Sync or Drift-Detection Latency

**Symptom:** The `MCPServerHighGitOpsSyncLatencyP95` or
`MCPServerHighGitOpsDriftLatencyP95` alert in
[`docs/alerts/mcpserver-rules.yaml`](../docs/alerts/mcpserver-rules.yaml)
has fired. Both are `kubestellar-deploy`-only — `kubestellar-ops` never
calls the GitOps `Syncer` or drift detector.

### Steps

1. Enable the metrics endpoint if it is not already running for this
   deployment (see [Using the Metrics Endpoint](#using-the-metrics-endpoint)
   above).

2. Isolate the affected cluster:
   ```bash
   curl -s http://127.0.0.1:9090/metrics | grep 'mcpserver_gitops_sync_duration_seconds\|mcpserver_gitops_drift_duration_seconds'
   ```
   Compare `mcpserver_gitops_sync_duration_seconds{cluster}` and
   `mcpserver_gitops_drift_duration_seconds{cluster}` across clusters — a
   spike concentrated on one `cluster` label usually points to that
   cluster's API server rather than the MCP server itself.

3. If latency is concentrated on one cluster: check that cluster's API
   server responsiveness directly (`kubectl --context <context-name> get
   --raw='/readyz?verbose'`) and follow
   [Multi-Cluster Connectivity Loss](#multi-cluster-connectivity-loss) if
   it is unreachable or degraded.

4. For sync latency specifically: a stalled sync can still eventually
   succeed, so `MCPServerHighGitOpsSyncFailureRate` may not have fired —
   treat this alert as an independent early-warning signal, not just a
   precursor to failures.

5. For drift-detection latency: cross-reference `gitops drift check
   failed` log lines (see
   [`docs/alerts/README.md`](../docs/alerts/README.md)'s note on
   `mcpserver_gitops_drift_total`) — a slow drift check against a
   degraded cluster can also surface as elevated `missing` counts from
   `checkResource` errors, not just elevated latency.

6. If latency spans multiple clusters: check for a recent
   `kubestellar-deploy` binary/image upgrade, and follow
   [Diagnosing Silent Failures](#diagnosing-silent-failures) for
   panic/log inspection.

---

## Diagnosing High GitOps Sync Failure Rate or Multi-Cluster Fan-Out Failures/Latency

**Symptom:** The `MCPServerHighGitOpsSyncFailureRate`,
`MCPServerHighMulticlusterOperationFailureRate`, or
`MCPServerHighMulticlusterOperationLatencyP95` alert in
[`docs/alerts/mcpserver-rules.yaml`](../docs/alerts/mcpserver-rules.yaml)
has fired. All three are `kubestellar-deploy`-only — `kubestellar-ops`
never calls the GitOps `Syncer` or constructs a `multicluster.Executor`.

### Steps

1. Enable the metrics endpoint if it is not already running for this
   deployment (see [Using the Metrics Endpoint](#using-the-metrics-endpoint)
   above).

2. Isolate the affected cluster:
   ```bash
   curl -s http://127.0.0.1:9090/metrics | grep 'mcpserver_gitops_sync_total\|mcpserver_multicluster_operation_total\|mcpserver_multicluster_operation_duration_seconds'
   ```
   Compare `mcpserver_gitops_sync_total{cluster,action="failed"}` and
   `mcpserver_multicluster_operation_total{cluster,status="error"}` across
   clusters — a spike concentrated on one `cluster` label usually points
   to that cluster's API server rather than the MCP server itself.

3. For `MCPServerHighGitOpsSyncFailureRate`: a failed sync still records
   per-resource outcomes, so check which resources failed via
   `kubectl --context <context-name> get events` on the target cluster
   before assuming the MCP server itself is at fault.

4. For `MCPServerHighMulticlusterOperationFailureRate`: this reflects a
   per-cluster failure within a fan-out call — other clusters in the same
   call can still succeed. Treat a failure concentrated on one `cluster`
   label as that cluster's problem, not a systemic one.

5. For `MCPServerHighMulticlusterOperationLatencyP95`: a slow/degraded
   cluster in a fan-out call can stall and still eventually record
   `status="success"`, so this can fire independently of
   `MCPServerHighMulticlusterOperationFailureRate` — treat it as an
   early-warning signal, not just a precursor to failures.

6. If the affected cluster is unreachable or degraded, follow
   [Multi-Cluster Connectivity Loss](#multi-cluster-connectivity-loss).

7. If failures/latency span multiple clusters: check for a recent
   `kubestellar-deploy` binary/image upgrade, and follow
   [Diagnosing Silent Failures](#diagnosing-silent-failures) for
   panic/log inspection.

---

## Diagnosing Blocked-IP (SSRF Guard) Attempts

**Symptom:** The `MCPServerBlockedIPAttempts` alert in
[`docs/alerts/mcpserver-rules.yaml`](../docs/alerts/mcpserver-rules.yaml)
has fired. At least one tool call was rejected by
`pkg/security/netguard` because a Helm chart/repo or GitOps repo URL
resolved to a private/internal address.

### Steps

1. Enable the metrics endpoint if it is not already running for this
   deployment (see [Using the Metrics Endpoint](#using-the-metrics-endpoint)
   above).

2. Isolate the affected tool and cluster:
   ```bash
   curl -s http://127.0.0.1:9090/metrics | grep 'mcpserver_tool_errors_total{.*error_kind="blocked_ip"'
   ```
   `mcpserver_tool_errors_total{tool,cluster,error_kind="blocked_ip"}`
   identifies which tool (and, if scoped, cluster) triggered the guard.

3. Find the specific rejected URL in the structured tool-call-failed log
   line (see [Diagnosing Silent Failures](#diagnosing-silent-failures) for
   how to capture logs): `klog.ErrorS(nil, "tool call failed", "tool",
   ...)` does not include the URL itself (the metric label set is bounded
   and never carries raw URLs), so check the originating request's
   arguments or, for GitOps/Helm deploy tools, the deployment manifest
   that supplied the chart/repo reference.

4. Determine intent:
   - **Misconfiguration** — an internal Git/Helm mirror or registry was
     referenced by its private address. Either route it through an
     allowed public endpoint or have an operator explicitly accept the
     risk out-of-band; the guard in `pkg/security/netguard` has no
     allowlist override.
   - **Possible probing** — the resolved URL has no plausible operator
     origin (e.g. link-local or cloud metadata addresses such as
     `169.254.169.254`). Treat as a potential SSRF attempt: check which
     MCP client/credential issued the request and whether other blocked
     attempts cluster around the same caller.

5. This alert fires on any non-zero count in a 15m window (not a ratio),
   so a single occurrence does not necessarily indicate an ongoing
   problem — confirm via step 2 whether attempts are repeating before
   escalating.

---

## Detecting a Failed Scheduled Workflow (Security Scans, Stale Triage, Release, Build/Test)

**Symptom:** No symptom is surfaced automatically — this is the problem. `codeql.yml`
(weekly, Monday 04:00 UTC), `scorecard.yml` (weekly, Monday 06:00 UTC),
`stale.yml` (daily, midnight UTC), `release.yml` (nightly 05:00 UTC and
weekly Sunday 05:00 UTC), `build-test.yml` (daily, 06:00 UTC), and
`fuzz.yml` (weekly, Sunday 07:00 UTC) all run unattended on a cron schedule
in addition to their other triggers, and
`release.yml`'s `notify` job (as of
[#865](https://github.com/kubestellar/kubestellar-mcp/issues/865)) and
`build-test.yml`'s `notify` job (as of
[#1216](https://github.com/kubestellar/kubestellar-mcp/pull/1216), closing
[#1214](https://github.com/kubestellar/kubestellar-mcp/issues/1214)) now
have an `if: failure()`-equivalent step that opens an alert-labeled issue
on a failed *scheduled* run; `codeql.yml` and `scorecard.yml` (tracked in
[#730](https://github.com/kubestellar/kubestellar-mcp/issues/730)),
`stale.yml` (tracked in
[#753](https://github.com/kubestellar/kubestellar-mcp/issues/753)), and
`fuzz.yml` (tracked in
[#1241](https://github.com/kubestellar/kubestellar-mcp/issues/1241)) still
lack an equivalent alert step. For those, a failed scheduled run is still
visible only as a red X in the Actions tab, so a failure can go unnoticed
indefinitely unless someone is watching. `build-test.yml`'s daily run was
a particularly important case before #1216: per `docs/slo.md` "Alerting
Guidance", it is the only automated check that re-validates SLO 2
(Cluster Discovery Latency) and SLO 4 (Tool-Call Accuracy) against
environmental drift during windows with no commits — a silent failure
there would mean that drift-detection signal goes dark with no one aware
of it. `fuzz.yml`'s weekly run is similarly the only automated check
continuously exercising `FuzzValidateRepoURL`, `FuzzValidateBranchName`,
`FuzzValidateHelmIdentifier`, `FuzzValidateHelmSetKey`,
`FuzzValidateHelmSetValue`, `FuzzValidateNamespace`, and
`FuzzSanitizeControlChars` against newly generated inputs — a silent
failure there means that input-validation/sanitization regression net is
down with no one aware, for an unbounded number of weeks.

### Interim manual safeguards (for `codeql.yml`, `scorecard.yml`, `stale.yml`, and `fuzz.yml`, until an automated alert exists)

1. **Enable per-repo/per-user "Failed workflows only" notifications:** GitHub
   Settings → Notifications → Actions → "Only notify for failed workflows".
   This surfaces a failed scheduled run in your notification feed without
   needing to poll the Actions tab.
2. **Periodically check scheduled-run status directly:**
   ```bash
   gh run list --repo kubestellar/kubestellar-mcp --workflow codeql.yml --limit 5
   gh run list --repo kubestellar/kubestellar-mcp --workflow scorecard.yml --limit 5
   gh run list --repo kubestellar/kubestellar-mcp --workflow stale.yml --limit 5
   gh run list --repo kubestellar/kubestellar-mcp --workflow release.yml --limit 5
   gh run list --repo kubestellar/kubestellar-mcp --workflow fuzz.yml --limit 5
   ```
   A `failure` conclusion on the most recent scheduled (non-push, non-PR,
   non-`workflow_dispatch`) run means the scan/triage did not complete;
   investigate before assuming everything is up to date.
3. **If CodeQL's weekly run has failed silently:** treat `main` as unscanned
   for the affected window. Re-run manually via `workflow_dispatch` once the
   underlying failure (e.g. a toolchain or query-pack change) is fixed, rather
   than waiting for the next Monday's cron.
4. **If Scorecard's weekly run has failed silently:** the public
   supply-chain score badge may be stale rather than reflecting current
   `main`. Do not treat an unexpectedly high/unchanged score as confirmation
   of a clean posture without checking the run actually succeeded.
5. **If `stale.yml`'s daily run has failed silently:** issue/PR staleness
   labeling and auto-closing (delegated to `kubestellar/infra`'s
   `reusable-stale.yml`) has stopped accumulating repo-wide. This runs daily,
   so a silent failure compounds faster than the weekly scans above — check
   run status more frequently, and re-run manually via `workflow_dispatch`
   once the underlying failure is fixed rather than waiting for the next
   midnight cron.
6. **`release.yml`'s scheduled runs are now auto-alerted** (see
   [`runbooks/release-rollback.md`](release-rollback.md) §0), but manual
   `workflow_dispatch` runs are not, so still check those directly:
   ```bash
   gh run list --repo kubestellar/kubestellar-mcp --workflow release.yml --limit 5
   ```
   `ghcr-publish.yml` and the Homebrew tap publish step are downstream of a
   successful `release.yml` run, so a `release.yml` failure silently means
   those never fire either. Follow
   [`runbooks/release-rollback.md`](release-rollback.md) if a *bad* (not
   failed) release shipped instead.
7. **`build-test.yml`'s daily scheduled run is now auto-alerted** (since
   [#1216](https://github.com/kubestellar/kubestellar-mcp/pull/1216)): its
   `notify` job opens a `build-test-alert`-labeled issue when
   `github.event_name == 'schedule'` and any of `build`,
   `validate-server-json`, or `lint` fail, mirroring `release.yml`'s
   pattern. Manual `workflow_dispatch` runs still need a direct check:
   ```bash
   gh run list --repo kubestellar/kubestellar-mcp --workflow build-test.yml --limit 5
   ```
   A `failure` conclusion means `go build -v ./...` and/or the integration
   test suite failed outside any code change — e.g. a Go toolchain update, a
   transitive dependency regression, or a flaky `envtest` binary. Per
   `docs/slo.md` "Alerting Guidance", this run is the only mechanism
   re-validating SLO 2 (Cluster Discovery Latency) and SLO 4 (Tool-Call
   Accuracy) against environmental drift during windows with no commits.
8. **The `notify` jobs in `build-test.yml` and `release.yml` are themselves
   currently unmonitored** (tracked in
   [#1245](https://github.com/kubestellar/kubestellar-mcp/issues/1245)): if a
   `notify` job's own `github-script` step errors — a token-permission
   change, an Actions API change, a typo introduced in a future edit — it can
   fail silently and the scheduled-run-failure alert for that workflow stops
   firing with no one aware, the same "no symptom surfaced" problem `notify`
   exists to prevent. `kubestellar/homebrew-tap` closed the equivalent gap for
   its own alerting via an `alert-canary.yml` workflow
   ([homebrew-tap#709](https://github.com/kubestellar/homebrew-tap/pull/709));
   #1245 proposes the matching workflow for this repo but cannot land it
   directly (workflow files need a maintainer or merge-capable agent to push).
   Until it lands, periodically spot-check both `notify` jobs directly:
   ```bash
   gh run view --repo kubestellar/kubestellar-mcp --job <job-id>  # notify job from the latest scheduled run
   ```
   A `failure` conclusion on `notify` itself (not just on `build`/`lint`/etc.)
   means the alert step broke, not just the thing it was watching.
9. **If `fuzz.yml`'s weekly run has failed silently** (tracked in
   [#1241](https://github.com/kubestellar/kubestellar-mcp/issues/1241), not
   yet auto-alerted): the `FuzzValidateRepoURL`, `FuzzValidateBranchName`,
   `FuzzValidateHelmIdentifier`, `FuzzValidateHelmSetKey`,
   `FuzzValidateHelmSetValue`, `FuzzValidateNamespace`, and
   `FuzzSanitizeControlChars` targets have stopped being exercised against
   new inputs for at least a week. A `failure` conclusion can mean either a
   genuine crasher was found (check the uploaded `fuzz-crashers-*` artifact
   on the failed run) or an environmental issue (Go toolchain update,
   runner regression). Re-run manually via `workflow_dispatch` (with an
   explicit `fuzztime` input) once triaged, rather than waiting for the
   next Sunday's cron.

## Detecting a Broken PR-Gating Check (`pull_request_target` startup_failure)

**Current status:** fixed since `pr-verifier.yml` was restored to a
self-contained, fork-guarded check in
[#923](https://github.com/kubestellar/kubestellar-mcp/pull/923) (merged
2026-09-17), which removed the dependency on
`kubestellar/infra/.github/workflows/reusable-pr-verifier.yml` entirely —
the workflow no longer has a `uses:` reference that can go missing
upstream. Recent runs are `success`, not `startup_failure`. This section is
kept because the failure mode below has recurred twice already
([#567](https://github.com/kubestellar/kubestellar-mcp/issues/567),
[#877](https://github.com/kubestellar/kubestellar-mcp/issues/877)); if
`pr-verifier.yml` is ever changed back to call a reusable workflow, the
same class of outage can reoccur.

**Symptom:** Every PR shows a red X on the **PR Verifier** check
(`.github/workflows/pr-verifier.yml`), but the failure is a
`startup_failure` — zero jobs actually run, so no title-format validation
happens at all. This is different from the scheduled-workflow gap above: it
fires on *every* PR (making it highly visible), yet because it fails before
any job starts, reviewers can misread the red X as "the check ran and the
title is wrong" when in fact the check never ran.

**Root cause (of the past occurrences):** `pr-verifier.yml`'s only job
called a reusable workflow —
`uses: kubestellar/infra/.github/workflows/reusable-pr-verifier.yml@<sha>` —
that did not exist in `kubestellar/infra` at the pinned SHA or at `main`.
This recurred at least twice: first reported and closed via
[#567](https://github.com/kubestellar/kubestellar-mcp/issues/567) (fixed by
re-pinning the `uses:` SHA), then found broken again by the same symptom in
[#877](https://github.com/kubestellar/kubestellar-mcp/issues/877) —
re-pinning the SHA did not help because the target file still didn't exist
at that revision in `infra`. [#923](https://github.com/kubestellar/kubestellar-mcp/pull/923)
resolved this by dropping the reusable-workflow dependency altogether
rather than re-pinning it again.

### Diagnosis steps

```bash
gh run list --repo kubestellar/kubestellar-mcp --workflow=pr-verifier.yml --limit 5
gh run view <run-id> --repo kubestellar/kubestellar-mcp --log-failed
```

A `startup_failure` conclusion (not `failure`) with "This run likely failed
because of a workflow file issue" means the reusable workflow reference is
broken again — check whether
`kubestellar/infra/.github/workflows/reusable-pr-verifier.yml` exists at the
SHA/ref `pr-verifier.yml` currently points to.

### Interim manual safeguards (if this recurs)

1. **Do not treat the red X as a title-format failure.** Open the run and
   confirm it is `startup_failure` with zero jobs executed before asking a
   contributor to "fix the title" — there may be nothing wrong with it.
2. **Manually validate the PR title** against the Conventional Commits
   pattern this check is supposed to enforce before merging:
   `^(\[(scanner|agent|ci-maintainer|quality|sec-check)\] )?(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\(.+\))?!?: .+`
3. **If `pr-verifier.yml` has been changed to call a reusable workflow
   again, do not re-pin the `uses:` SHA as a fix without first confirming
   the target file exists at that ref** (`gh api
   repos/kubestellar/infra/contents/.github/workflows/reusable-pr-verifier.yml?ref=<sha>`) —
   this is exactly how the #567 fix silently stopped working again. The
   more durable fix, applied in #923, is to keep the check self-contained
   instead of depending on `kubestellar/infra` at all.

## Escalation

| Condition | Action |
|-----------|--------|
| Server crashes repeatedly on startup | File a bug at https://github.com/kubestellar/kubestellar-mcp/issues |
| Cluster discovery returns wrong clusters | Verify kubeconfig; file a bug with kubeconfig excerpt (redact credentials) |
| Security concern (credential leak, RBAC bypass) | Follow https://github.com/kubestellar/kubestellar-mcp/blob/main/SECURITY.md |
| Data loss or incorrect drift detection | File a bug with reproducible steps |

**Issue tracker:** https://github.com/kubestellar/kubestellar-mcp/issues  
**Security policy:** https://github.com/kubestellar/kubestellar-mcp/blob/main/SECURITY.md
