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
8. [Escalation](#escalation)
9. [Release Rollback](release-rollback.md) (separate runbook, for a bad automated nightly/weekly release)

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

---

## Escalation

| Condition | Action |
|-----------|--------|
| Server crashes repeatedly on startup | File a bug at https://github.com/kubestellar/kubestellar-mcp/issues |
| Cluster discovery returns wrong clusters | Verify kubeconfig; file a bug with kubeconfig excerpt (redact credentials) |
| Security concern (credential leak, RBAC bypass) | Follow https://github.com/kubestellar/kubestellar-mcp/blob/main/SECURITY.md |
| Data loss or incorrect drift detection | File a bug with reproducible steps |

**Issue tracker:** https://github.com/kubestellar/kubestellar-mcp/issues  
**Security policy:** https://github.com/kubestellar/kubestellar-mcp/blob/main/SECURITY.md
