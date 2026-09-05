# Release Rollback Runbook

Covers what to do when an automated release from `.github/workflows/release.yml`
(and its downstream `ghcr-publish.yml` and Homebrew tap publish steps) ships a
regression. The release pipeline runs **unattended on a schedule** (nightly at
05:00 UTC, weekly on Sunday) in addition to manual `workflow_dispatch`, so a bad
release can both go undetected longer and be immediately superseded by the next
scheduled run before a fix lands. Follow these steps in order.

## 0. Detect a failed or bad unattended run

Nobody is paged when a scheduled `release.yml` run fails or ships a
regression — the workflow's `notify` job only writes a `GITHUB_STEP_SUMMARY`,
which nobody sees unless they open that specific run (tracked in
[#694](https://github.com/kubestellar/kubestellar-mcp/issues/694)). Until an
automated alert exists, use one of these to catch a bad unattended run before
this runbook's later steps are needed:

1. Per-user: **github.com → Settings → Notifications → Actions** → enable
   email/web notifications for "Failed workflows only" on repos you watch.
   This surfaces a failed nightly/weekly `release.yml` run in your inbox
   without anyone needing to open the Actions tab.
2. Manual check: review `https://github.com/kubestellar/kubestellar-mcp/actions/workflows/release.yml`
   for the most recent scheduled run's conclusion before relying on
   `latest`/Homebrew being current, especially after a reported user issue
   that "the fix isn't out yet."
3. A user-facing regression (not a workflow failure) is usually reported as a
   GitHub issue or in project chat rather than caught by CI — treat any such
   report as cause to jump straight to step 1 below rather than waiting for
   a workflow alert.

## 1. Stop the bleeding: pause the next scheduled run

The nightly/weekly cron in `release.yml` will otherwise re-publish before a fix
merges.

1. Go to **Actions → Release → ⋯ → Disable workflow** in the
   `kubestellar/kubestellar-mcp` repo. This blocks both the cron triggers and
   manual `workflow_dispatch` runs until re-enabled.
2. Post in the incident channel/issue that the Release workflow is disabled and
   why, with a reminder to re-enable it once the fix is verified.
3. Re-enable the workflow only after the corrected version has been built,
   tested, and is ready to ship (or after confirming the regression does not
   warrant blocking releases).

## 2. Mark the bad GitHub Release

1. Open the bad release under **Releases** in the repo.
2. Edit the release: prepend the title with `[YANKED]` and add a note at the
   top of the release body pointing to the version that fixes the regression
   (or stating a fix is pending).
3. Check **"Set as a pre-release"** so it no longer shows as "Latest" on the
   releases page, even if it was a stable (non-nightly) release.
4. Do **not** delete the release or its git tag — downstream consumers
   (Homebrew formula, GHCR image tags, docs version branch) reference it by
   tag, and deleting it breaks those references without removing the
   already-distributed artifacts.

## 3. Roll back the GHCR image

`ghcr-publish.yml` pushes `ghcr.io/kubestellar/kubestellar-mcp` tagged with the
exact version, the `{major}.{minor}` floating tag, and — for any non-nightly
release — the mutable `latest` tag. A bad stable release means `latest` now
points at broken code.

1. Identify the last known-good version tag (the stable release immediately
   before the bad one).
2. Re-run `ghcr-publish.yml` via `workflow_dispatch`, passing the known-good
   tag as the `tag` input. This rebuilds and re-pushes that version's image,
   which overwrites the `latest` tag (and matching `{major}.{minor}` tag) back
   to the known-good digest.
3. Confirm with `docker manifest inspect ghcr.io/kubestellar/kubestellar-mcp:latest`
   that the digest now matches the known-good build, not the bad one.
4. Leave the bad version's own immutable version tag (e.g. `:0.9.3`) in place
   for forensic/reproducibility purposes — only `latest` and the shared
   `{major}.{minor}` tag need to be moved off of it.

## 4. Revert the Homebrew formula

GoReleaser pushes updated formulas for `kubestellar-ops` and
`kubestellar-deploy` directly to `Formula/` in `kubestellar/homebrew-tap` on
every release.

1. In `kubestellar/homebrew-tap`, find the commit that bumped the affected
   formula(s) to the bad version.
2. Open a revert PR restoring the previous formula content (previous `url`,
   `sha256`, and `version`). Do not merge it yourself — operations agents
   never merge their own PRs; tag a maintainer for review given the
   user-facing urgency.
3. Once merged, `brew upgrade kubestellar-ops`/`kubestellar-deploy` will
   reinstall the known-good version for anyone who has not already upgraded,
   and stops the bad version from being newly installed.

## 5. Check the docs version branch dispatch

For non-prerelease (stable) versions, `release.yml` fires a
`create-version-branch` repository_dispatch to `kubestellar/docs`, which cuts
a new versioned docs branch there.

1. If the bad release only affects runtime behavior (not documented
   CLI/API surface), no docs action is needed.
2. If the bad release's docs branch describes now-inaccurate behavior (e.g. a
   flag or tool that doesn't work as documented), open an issue in
   `kubestellar/docs` referencing this rollback and the affected version
   branch so the docs team can annotate or hold that branch until the fix
   ships.

## 6. Ship the fix

1. Merge the fix through the normal PR process.
2. Re-enable the Release workflow (step 1) and trigger a `patch` release via
   `workflow_dispatch` rather than waiting for the next cron run, so the fix
   reaches users as quickly as the original bad release did.
3. Update the yanked release's notes (step 2) with a link to the fixed
   version once it is published.

## 7. Postmortem

File a postmortem using `docs/postmortem-template.md` for any rollback that
reached users (i.e. anything beyond a caught-in-CI failure). Reference the
yanked release tag, the GHCR/Homebrew rollback actions taken, and any gaps
this runbook didn't cover.
