# Branch Protection Policy

This document records the required branch-protection settings for
`kubestellar/kubestellar-mcp`.  Repository administrators must apply these
settings in **Settings → Branches → Branch protection rules** for the `main`
branch.

These controls address the findings raised by the OpenSSF Scorecard
`Branch-Protection` (issue #459) and `Code-Review` (issue #460) checks.

## Required settings

| Setting | Value |
|---------|-------|
| Require a pull request before merging | **enabled** |
| Required approving reviews | **1** (minimum) |
| Dismiss stale reviews when new commits are pushed | **enabled** |
| Require review from code owners (`CODEOWNERS`) | **enabled** |
| Require status checks to pass before merging | **enabled** |
| Require branches to be up to date before merging | **enabled** |
| Do not allow bypassing the above settings | **enabled** (applies to admins too) |
| Allow force pushes | **disabled** |
| Allow deletions | **disabled** |

## Rationale

* **Force-push / deletion protection** prevents history rewriting on `main`,
  which is the vector described in Scorecard's `Branch-Protection` check.
* **Required review** ensures at least one human inspects every change before
  it lands, addressing the `Code-Review` finding.
* **Status-check gating** keeps broken builds and failing CI from reaching
  `main`.

## Enforcement

These settings cannot be committed to the repository — they must be applied via
the GitHub UI or the GitHub API by a repository admin.  This file serves as the
authoritative policy record so the configuration is auditable in version
control.

To apply via the GitHub API (requires admin token):

```bash
curl -X PUT \
  -H "Authorization: Bearer $GITHUB_ADMIN_TOKEN" \
  -H "Accept: application/vnd.github+json" \
  https://api.github.com/repos/kubestellar/kubestellar-mcp/branches/main/protection \
  -d '{
    "required_status_checks": {
      "strict": true,
      "contexts": []
    },
    "enforce_admins": true,
    "required_pull_request_reviews": {
      "dismiss_stale_reviews": true,
      "require_code_owner_reviews": true,
      "required_approving_review_count": 1
    },
    "restrictions": null,
    "allow_force_pushes": false,
    "allow_deletions": false
  }'
```

Fixes #459 and #460.
