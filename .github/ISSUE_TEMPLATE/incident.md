---
name: Incident Report
about: Report a service-impacting incident
title: "[incident] <short description>"
labels: ""
assignees: ""
---

<!--
NOTE: this template intentionally applies no labels via front matter. It
previously set `labels: "kind/incident"`, but that label does not exist in
this repository, and GitHub silently drops unknown labels named in
issue-template front matter at creation time (no error, no warning) — so
every incident issue filed via this template got created with zero labels
applied. See #886 for the confirmed gap and the request for a maintainer
to create `kind/incident`. Once that label exists, restore
`labels: "kind/incident"` above in a follow-up PR.
-->

## Incident Summary

**Date/Time (UTC):**  
**Duration:**  
**Severity:** <!-- P1 / P2 / P3 / P4 — see ../../docs/severity-levels.md for definitions -->  
**Status:** <!-- Investigating / Mitigated / Resolved -->

## Impact

<!-- What was affected? How many users/clusters? What functionality was degraded or unavailable? -->

## Timeline

| Time (UTC) | Event |
|------------|-------|
|            | Incident detected |
|            | Investigation started |
|            | Root cause identified |
|            | Mitigation applied |
|            | Resolved |

## Root Cause

<!-- Describe the technical root cause. Be specific. -->

## Mitigation / Resolution

<!-- What was done to stop the bleeding and restore service? -->

## Contributing Factors

<!-- What conditions allowed this to happen? (e.g., missing health checks, no alerting, untested code path) -->

## Action Items

| Action | Owner | Due Date | Issue |
|--------|-------|----------|-------|
|        |       |          |       |

## Lessons Learned

<!-- What did we learn? What should we do differently? -->

## Postmortem

**Postmortem required?** <!-- Yes (P1/P2 per ../../docs/severity-levels.md, or any user-impacting rollback per runbooks/release-rollback.md) / No (P3/P4) -->  
**Postmortem link:** <!-- File using docs/postmortem-template.md, e.g. docs/postmortems/YYYY-MM-DD-<short-title>.md, and link it here once opened -->
