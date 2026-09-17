#!/bin/bash
# scripts/run_shell_tests.sh
#
# Discover and run every scripts/__tests__/*.test.sh file, aggregate results,
# and exit non-zero if any test file failed. This is what wires the existing
# shell tests (previously present in scripts/__tests__/ but never invoked by
# CI) into the build-test workflow, so a regression to any of the coverage
# ratchet / package coverage / ratchet completeness scripts is caught pre-merge
# instead of only on a live workflow run.
#
# Usage:
#   scripts/run_shell_tests.sh [test-dir]
#
# test-dir defaults to scripts/__tests__ (relative to this script). Any file
# matching *.test.sh under that directory is executed with bash. The script
# exits 0 iff every discovered test file exits 0, 1 otherwise. Exit 2 is
# reserved for usage errors (missing directory, no tests discovered).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TEST_DIR="${1:-${SCRIPT_DIR}/__tests__}"

if [ ! -d "$TEST_DIR" ]; then
  echo "run_shell_tests.sh: test directory not found: $TEST_DIR" >&2
  exit 2
fi

# Collect *.test.sh files (sorted for deterministic ordering). Use find so we
# handle empty directories cleanly and don't rely on shopt nullglob.
mapfile -t TEST_FILES < <(find "$TEST_DIR" -maxdepth 1 -type f -name '*.test.sh' | sort)

if [ "${#TEST_FILES[@]}" -eq 0 ]; then
  echo "run_shell_tests.sh: no *.test.sh files found under $TEST_DIR" >&2
  exit 2
fi

FAILED_FILES=()
PASSED_COUNT=0

for test_file in "${TEST_FILES[@]}"; do
  rel="${test_file#"$SCRIPT_DIR"/}"
  echo "──────────────────────────────────────────"
  echo "▶ ${rel}"
  echo "──────────────────────────────────────────"
  if bash "$test_file"; then
    PASSED_COUNT=$((PASSED_COUNT + 1))
  else
    FAILED_FILES+=("$rel")
  fi
done

echo ""
echo "══════════════════════════════════════════"
if [ "${#FAILED_FILES[@]}" -eq 0 ]; then
  echo "✅ shell tests: ${PASSED_COUNT} / ${#TEST_FILES[@]} files passed"
  echo "══════════════════════════════════════════"
  exit 0
fi

echo "❌ shell tests: ${#FAILED_FILES[@]} of ${#TEST_FILES[@]} files failed"
for f in "${FAILED_FILES[@]}"; do
  echo "  - $f"
done
echo "══════════════════════════════════════════"
exit 1
