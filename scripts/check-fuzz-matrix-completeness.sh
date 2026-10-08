#!/bin/bash
# scripts/check-fuzz-matrix-completeness.sh
#
# Reports Go fuzz targets (func FuzzXxx(f *testing.F) in *_test.go files)
# that are NOT listed in .github/workflows/fuzz.yml's matrix. A fuzz target
# missing from the matrix is never exercised by the weekly scheduled fuzz
# run, so a regression it would have caught can ship silently
# (kubestellar-mcp#1169).
#
# By default the script only reports (exit 0). Pass --strict (or set
# STRICT=1) to exit non-zero when any target is missing, so it can be wired
# into CI as a hard gate, mirroring check-go-ratchet-completeness.sh.
#
# Usage:
#   ./scripts/check-fuzz-matrix-completeness.sh <fuzz-workflow-file> [<root-dir>...] [--strict]
#
# Defaults: roots = "pkg cmd internal" if no roots supplied.

set -euo pipefail

STRICT="${STRICT:-0}"
ARGS=()
for arg in "$@"; do
  case "$arg" in
    --strict) STRICT=1 ;;
    *) ARGS+=("$arg") ;;
  esac
done

if [ "${#ARGS[@]}" -lt 1 ]; then
  echo "Usage: $0 <fuzz-workflow-file> [<root-dir>...] [--strict]" >&2
  exit 2
fi

WORKFLOW_FILE="${ARGS[0]}"
if [ ! -f "$WORKFLOW_FILE" ]; then
  echo "Fuzz workflow file not found: $WORKFLOW_FILE" >&2
  exit 2
fi

ROOTS=("${ARGS[@]:1}")
if [ "${#ROOTS[@]}" -eq 0 ]; then
  ROOTS=(pkg cmd internal)
fi

# Targets already wired into the workflow's matrix (one `target: FuzzXxx`
# per matrix entry).
WIRED=$(grep -oE 'target:[[:space:]]*Fuzz[A-Za-z0-9_]+' "$WORKFLOW_FILE" \
  | awk '{print $2}' | sort -u)

MISSING=""
MISSING_COUNT=0

# A fuzz target is any `func FuzzXxx(f *testing.F)` in a *_test.go file,
# skipping vendored/third-party trees.
find_fuzz_targets() {
  for root in "${ROOTS[@]}"; do
    [ -d "$root" ] || continue
    find "$root" \
      -type d \
      \( -name vendor -o -name testdata -o -name node_modules -o -name .git \) -prune -o \
      -type f -name '*_test.go' -print 2>/dev/null
  done | while read -r gofile; do
    grep -oE '^func (Fuzz[A-Za-z0-9_]+)\(f \*testing\.F\)' "$gofile" 2>/dev/null \
      | sed -E 's/^func ([A-Za-z0-9_]+).*/\1/' || true
  done | sort -u
}

while IFS= read -r target; do
  [ -z "$target" ] && continue
  if ! printf '%s\n' "$WIRED" | grep -qxF "$target"; then
    MISSING="${MISSING}${target}"$'\n'
    MISSING_COUNT=$((MISSING_COUNT + 1))
  fi
done < <(find_fuzz_targets)

if [ "$MISSING_COUNT" -eq 0 ]; then
  echo "All Go fuzz targets under ${ROOTS[*]} are listed in ${WORKFLOW_FILE}."
  exit 0
fi

echo "::warning::${MISSING_COUNT} Go fuzz target(s) are missing from ${WORKFLOW_FILE}'s matrix:"
printf '%s' "$MISSING" | sed 's/^/  - /'

if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  {
    echo "## Fuzz matrix completeness"
    echo ""
    echo "**${MISSING_COUNT}** Go fuzz target(s) are missing from \`${WORKFLOW_FILE}\`'s matrix:"
    echo ""
    printf '%s' "$MISSING" | sed 's/^/- `/; s/$/`/'
  } >> "$GITHUB_STEP_SUMMARY"
fi

if [ "$STRICT" = "1" ]; then
  echo "::error::Fuzz matrix is incomplete (${MISSING_COUNT} target(s) unlisted); add them to ${WORKFLOW_FILE} or disable --strict."
  exit 1
fi

exit 0
