---
name: Incident Report
about: Report a service-impacting incident
title: "[incident] <short description>"
assignees: ""
---

<!--
  Note: this template previously listed `labels: "kind/incident"` in its
  front matter, but that label does not exist in this repository. GitHub
  silently drops unknown labels named in issue-template front matter at
  creation time, so every incident filed via this template was created
  with zero labels applied. The front matter label was removed here to
  stop implying a label is applied that isn't. See #886: once a
  maintainer creates the `kind/incident` label, restore
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
