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
9. [Detecting a Failed Scheduled Workflow (Security Scans, Stale Triage)](#detecting-a-failed-scheduled-workflow-security-scans-stale-triage)
10. [Escalation](#escalation)
11. [Release Rollback](release-rollback.md) (separate runbook, for a bad automated nightly/weekly release)

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

The container runs as a non-root user (`nonroot:65532`). The MCP server's primary interface is stdio transport, which has no HTTP endpoint to probe. However, if the operator started the server with `--metrics-addr` (see [Using the Metrics Endpoint](#using-the-metrics-endpoint) below), a `/healthz` liveness endpoint is also available on that same opt-in listener:

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:9090/healthz
# Expected: 200
```

`/healthz` only confirms the metrics HTTP listener itself is alive — it does not check any downstream dependency (e.g. cluster API reachability), so a 200 here is not a substitute for the `mcpserver_tool_errors_total` / `mcpserver_active_clusters` checks below. When `--metrics-addr` is not set (the default), fall back to the process-level checks:

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

**Availability:** The `/metrics` endpoint is opt-in. It is only served when the operator passes `--metrics-addr <host:port>` at startup; by default no listener is started and no metrics are exposed (see `pkg/metrics`). The same listener also serves `/healthz` (see [Container Health Verification](#container-health-verification) above) — a plain liveness check with no dependency gating.

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

### Dashboard

A ready-to-import Grafana dashboard for these metrics is at
[`docs/dashboards/mcpserver-overview.json`](../docs/dashboards/mcpserver-overview.json)
(see [`docs/dashboards/README.md`](../docs/dashboards/README.md)). It requires a
Prometheus instance already scraping this server's `/metrics` endpoint — no
scrape config or backend is bundled with this repository.

---

## Detecting a Failed Scheduled Workflow (Security Scans, Stale Triage)

**Symptom:** No symptom is surfaced automatically — this is the problem. `codeql.yml`
(weekly, Monday 04:00 UTC), `scorecard.yml` (weekly, Monday 06:00 UTC), and
`stale.yml` (daily, midnight UTC) all run unattended on a cron schedule in
addition to their other triggers, and none of them has a step that alerts a
human on failure (tracked in [#730](https://github.com/kubestellar/kubestellar-mcp/issues/730)
for `codeql.yml`/`scorecard.yml` and [#753](https://github.com/kubestellar/kubestellar-mcp/issues/753)
for `stale.yml`, the same gap class as the release-workflow alert gap in
[#694](https://github.com/kubestellar/kubestellar-mcp/issues/694)). A failed
scheduled run is visible only as a red X in the Actions tab, so it can go
unnoticed indefinitely unless someone is watching.

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

## Escalation

| Condition | Action |
|-----------|--------|
| Server crashes repeatedly on startup | File a bug at https://github.com/kubestellar/kubestellar-mcp/issues |
| Cluster discovery returns wrong clusters | Verify kubeconfig; file a bug with kubeconfig excerpt (redact credentials) |
| Security concern (credential leak, RBAC bypass) | Follow https://github.com/kubestellar/kubestellar-mcp/blob/main/SECURITY.md |
| Data loss or incorrect drift detection | File a bug with reproducible steps |

**Issue tracker:** https://github.com/kubestellar/kubestellar-mcp/issues  
**Security policy:** https://github.com/kubestellar/kubestellar-mcp/blob/main/SECURITY.md
