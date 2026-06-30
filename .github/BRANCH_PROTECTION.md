# Branch Protection Configuration Guide

## Overview

This document provides the recommended branch protection settings for the `main` branch to achieve a minimum Scorecard security score of 6/10 (Tier 2) and protect against supply chain attacks.

## Required Settings

### Location
Settings → Branches → Branch protection rules → Add/Edit rule for `main`

### Core Requirements

1. **Require pull request reviews before merging**
   - ✅ Enable "Require a pull request before merging"
   - ✅ Required number of approvals: **1** (minimum)
   - ✅ Enable "Dismiss stale pull request approvals when new commits are pushed"
   - ✅ Enable "Require review from Code Owners" (CODEOWNERS file already exists)

2. **Require status checks to pass before merging**
   - ✅ Enable "Require status checks to pass before merging"
   - ✅ Enable "Require branches to be up to date before merging"
   - Required status checks to select:
     - `build` (from build-test.yml workflow)
     - `validate-server-json` (from build-test.yml workflow)
     - `lint` (from build-test.yml workflow)
     - `CodeQL` (from codeql.yml workflow)

3. **Additional protections**
   - ✅ Enable "Require conversation resolution before merging"
   - ✅ Enable "Require signed commits" (optional but recommended)
   - ✅ Enable "Require linear history" (optional but recommended)
   - ✅ Enable "Include administrators" — **Critical**: prevents admins from bypassing these rules

4. **Force push protection** (already enabled)
   - ✅ "Do not allow bypassing the above settings"
   - ✅ "Do not allow force pushes"
   - ✅ "Do not allow deletions"

## Security Impact

Without these protections:
- Compromised contributor accounts can push malicious code directly to `main`
- Code can be merged without peer review
- CI checks can be bypassed
- Supply chain attack surface is significantly increased

With these protections:
- All changes require peer review (at least 1 approver)
- All CI checks must pass before merge
- Direct pushes to `main` are prevented
- Administrators are subject to the same rules

## Current Status

As of issue #409:
- ❌ PRs not required before merging
- ❌ No required status checks
- ❌ Only 7% of recent commits were peer-reviewed (2 of 29)
- ✅ Force push prevention enabled
- ✅ Deletion prevention enabled
- **Current Scorecard score**: 3/10
- **Target Scorecard score**: 6/10 (Tier 2)

## Implementation

These settings can only be configured by repository administrators through the GitHub UI. They cannot be enforced via code or pull requests.

### Action Required
A repository administrator must:
1. Navigate to Settings → Branches
2. Add or edit the branch protection rule for `main`
3. Enable the settings listed above
4. Save the changes

### Verification
After applying these settings:
1. The Scorecard security check should improve from 3/10 to at least 6/10
2. Direct pushes to `main` will be blocked
3. All PRs will require at least 1 approval
4. All required status checks must pass before merge

## References

- [GitHub Branch Protection Documentation](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/about-protected-branches)
- [OSSF Scorecard Branch Protection Check](https://github.com/ossf/scorecard/blob/main/docs/checks.md#branch-protection)
- Issue #409: Branch protection score 3/10
