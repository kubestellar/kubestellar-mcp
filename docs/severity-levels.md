# Incident Severity Levels

This defines the P1–P4 scale referenced by the
[Incident Report template](../.github/ISSUE_TEMPLATE/incident.md) and the
[Postmortem template](postmortem-template.md), anchored to the SLOs in
[`docs/slo.md`](slo.md) and the
[MCP Server Operations Runbook](../runbooks/mcp-server-operations.md) /
[Release Rollback Runbook](../runbooks/release-rollback.md). Pick the
**highest** level that any criterion below applies to.

| Level | Criteria | Response expectation |
|-------|----------|----------------------|
| **P1** | Tool Response Availability (SLO 1) has dropped below the 7-day 90% floor, `mcpserver_active_clusters` has gone to 0 for reachable clusters, `check_cluster_health` (SLO 4) is giving materially wrong results, or a shipped release/image breaks the MCP server for all users | Begin mitigation/rollback immediately per [runbooks/mcp-server-operations.md](../runbooks/mcp-server-operations.md) and [runbooks/release-rollback.md](../runbooks/release-rollback.md); target restoring service within the SLO 1 error-budget window |
| **P2** | Tool error rate is elevated but above the 7-day floor (per the `MCPServerHighToolErrorRate` / `MCPServerCriticalToolErrorRate` alerts in [`docs/alerts/mcpserver-rules.yaml`](alerts/mcpserver-rules.yaml)), AI Provider Query Availability (SLO 5) is degraded, or a bad release affects a subset of users/clusters | Mitigate on the same incident-response cycle as P1; follow [runbooks/release-rollback.md](../runbooks/release-rollback.md) if a release is the cause |
| **P3** | A non-critical tool or feature is degraded (e.g., elevated latency that does not breach the SLO 2 objective, a cosmetic diagnostic inaccuracy) affecting only a handful of users/clusters | Fix on a normal PR cadence; rollback is optional if a workaround exists |
| **P4** | Cosmetic, documentation-only, or CI-infrastructure noise (e.g., a flaky scheduled scan) with no confirmed user-facing tool/runtime impact | Track as a normal issue; no incident response needed |

## When to write a postmortem

Per `docs/slo.md`'s Error Budget Policy, any **P1** or **P2** incident — or
any incident that fully exhausts the SLO 1 30-day error budget regardless of
assigned level — should get a [postmortem](postmortem-template.md). P3/P4
findings do not require one.
