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
9. [Diagnosing High Tool Error Rate or Latency](#diagnosing-high-tool-error-rate-or-latency)
10. [Diagnosing High AI Provider Query Error Rate or Latency](#diagnosing-high-ai-provider-query-error-rate-or-latency)
11. [Diagnosing a Metrics Scrape Target Down Alert](#diagnosing-a-metrics-scrape-target-down-alert)
12. [Detecting a Failed Scheduled Workflow (Security Scans, Stale Triage, Release)](#detecting-a-failed-scheduled-workflow-security-scans-stale-triage-release)
13. [Escalation](#escalation)
14. [Release Rollback](release-rollback.md) (separate runbook, for a bad automated nightly/weekly release)

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

The container runs as a non-root user (`nonroot:65532`). Because the MCP server uses stdio transport, there is no HTTP endpoint to probe. Use the following to verify the container is alive and responsive:

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

---

## Using the Metrics Endpoint

**Availability:** The `/metrics` endpoint is opt-in. It is only served when the operator passes `--metrics-addr <host:port>` at startup; by default no listener is started and no metrics are exposed (see `pkg/metrics`).

### Enabling for a diagnostic session

```bash
kubestellar-ops --mcp-server --metrics-addr 127.0.0.1:9090
curl -s http://127.0.0.1:9090/metrics | grep mcpserver_
```

### What to look for

- `mcpserver_tool_calls_total{tool,cluster,status}` — call volume and success/error split per tool and cluster.
- `mcpserver_tool_errors_total{tool,cluster,error_kind}` — error volume by tool, cluster, and a closed `error_kind` enum.
- `mcpserver_tool_duration_seconds{tool,cluster}` — latency histogram; compare against [SLO 1/2](../docs/slo.md) targets.
- `mcpserver_active_clusters` — reachable cluster count from the most recent discovery; a sudden drop indicates connectivity loss (see [Multi-Cluster Connectivity Loss](#multi-cluster-connectivity-loss)).
- `mcpserver_ai_query_total{provider,status}` / `mcpserver_ai_query_duration_seconds{provider}` — AI provider query volume, outcome, and latency (see `pkg/ai/claude/client.go`); watch alongside `MCPServerHighAIQueryErrorRate` in [`docs/alerts/mcpserver-rules.yaml`](../docs/alerts/mcpserver-rules.yaml).

### Dashboard

A ready-to-import Grafana dashboard for these metrics is at
[`docs/dashboards/mcpserver-overview.json`](../docs/dashboards/mcpserver-overview.json)
(see [`docs/dashboards/README.md`](../docs/dashboards/README.md)). It requires a
Prometheus instance already scraping this server's `/metrics` endpoint — no
scrape config or backend is bundled with this repository.

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
   curl -s http://127.0.0.1:9090/metrics | grep 'mcpserver_ai_query_total\|mcpserver_ai_query_duration_seconds'
   ```
   Compare `mcpserver_ai_query_total{provider,status}` and
   `mcpserver_ai_query_duration_seconds{provider}` across providers — a
   spike concentrated on one `provider` label points to that provider's
   endpoint rather than the MCP server itself.

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

## Diagnosing a Metrics Scrape Target Down Alert

**Symptom:** The `MCPServerMetricsScrapeTargetDown` alert in
[`docs/alerts/mcpserver-rules.yaml`](../docs/alerts/mcpserver-rules.yaml) has
fired. Every other alert in that file is computed from `mcpserver_*` samples
(rates, histograms, gauges); if the `/metrics` endpoint itself stops being
scraped, those queries simply run over stale or missing data instead of
alerting, so this is often the *only* signal that something is wrong even
when the underlying failure (crash, network partition) would also be
degrading SLO 1/2/5.

### Steps

1. Confirm the process is actually running per
   [Container Health Verification](#container-health-verification). If it
   exited or was OOM-killed, the scrape target going down is a symptom of
   that crash, not the root cause — start with
   [Diagnosing Silent Failures](#diagnosing-silent-failures).
2. If the process is running, check that `--metrics-addr` is still the
   flag the process was started with (see
   [Using the Metrics Endpoint](#using-the-metrics-endpoint)) and that
   nothing (network policy, firewall, port conflict) blocks Prometheus from
   reaching that host:port.
3. Check Prometheus's own target page (`/targets` in the Prometheus UI, or
   the equivalent in your Prometheus Operator/Grafana Agent setup) for this
   job. A `context deadline exceeded` or `connection refused` scrape error
   confirms a reachability problem; a target missing entirely from the list
   points to a service-discovery/label mismatch instead (check the `job`
   label used in `docs/alerts/mcpserver-rules.yaml` matches your actual
   ServiceMonitor/scrape config).
4. Once the endpoint is reachable again, confirm with:
   ```bash
   curl -s http://<metrics-addr>/metrics | grep mcpserver_
   ```
5. If the outage overlapped a period where tool calls were still being
   served (stdio traffic is independent of `--metrics-addr`), note the gap
   when reviewing SLO 1/2/5 error-budget consumption in
   [`docs/slo.md`](../docs/slo.md), since the missing samples mean the
   error budget was not being measured during that window, not that it was
   necessarily met.

---

## Detecting a Failed Scheduled Workflow (Security Scans, Stale Triage, Release)

**Symptom:** No symptom is surfaced automatically — this is the problem. `codeql.yml`
(weekly, Monday 04:00 UTC), `scorecard.yml` (weekly, Monday 06:00 UTC),
`stale.yml` (daily, midnight UTC), and `release.yml` (nightly 05:00 UTC and
weekly Sunday 05:00 UTC) all run unattended on a cron schedule in addition to
their other triggers, and none of them has a step that alerts a human on
failure (tracked in [#730](https://github.com/kubestellar/kubestellar-mcp/issues/730)
for `codeql.yml`/`scorecard.yml`, [#753](https://github.com/kubestellar/kubestellar-mcp/issues/753)
for `stale.yml`, and [#771](https://github.com/kubestellar/kubestellar-mcp/issues/771)
for `release.yml`, all the same gap class). A failed scheduled run is visible
only as a red X in the Actions tab — for `release.yml` the `notify` job's
`if: always()` step only ever writes a `GITHUB_STEP_SUMMARY`, which nobody is
watching at 5 AM UTC — so a failure can go unnoticed indefinitely unless
someone is watching.

> A same-shape automated fix (an `if: failure()` step in each workflow's
> terminal job that opens/updates a tracking issue via `gh issue create`) was
> drafted for `release.yml` but the push was rejected: `refusing to allow a
> GitHub App to create or update workflow` `.github/workflows/release.yml`
> `without` `workflows` `permission`. This token has `contents`/`issues`
> write but not the `workflows` scope required to touch files under
> `.github/workflows/`, so only this documentation-based interim safeguard
> can be delivered by automation; a maintainer with that scope should apply
> the workflow-file fix directly.

### Interim manual safeguards (until an automated alert exists)

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
6. **Check `release.yml`'s unattended runs directly:**
   ```bash
   gh run list --repo kubestellar/kubestellar-mcp --workflow release.yml --limit 5
   ```
   A `failure` conclusion on the most recent scheduled (non-`workflow_dispatch`)
   run means the nightly/weekly release did not ship. This is the fastest-moving
   gap of the four: `ghcr-publish.yml` and the Homebrew tap publish step are
   downstream of a successful `release.yml` run, so a `release.yml` failure
   silently means those never fire either. Follow
   [`runbooks/release-rollback.md`](release-rollback.md) if a *bad* (not
   failed) release shipped instead.

## Escalation

| Condition | Action |
|-----------|--------|
| Server crashes repeatedly on startup | File a bug at https://github.com/kubestellar/kubestellar-mcp/issues |
| Cluster discovery returns wrong clusters | Verify kubeconfig; file a bug with kubeconfig excerpt (redact credentials) |
| Security concern (credential leak, RBAC bypass) | Follow https://github.com/kubestellar/kubestellar-mcp/blob/main/SECURITY.md |
| Data loss or incorrect drift detection | File a bug with reproducible steps |

**Issue tracker:** https://github.com/kubestellar/kubestellar-mcp/issues  
**Security policy:** https://github.com/kubestellar/kubestellar-mcp/blob/main/SECURITY.md
