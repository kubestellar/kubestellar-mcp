#!/bin/bash
# scripts/__tests__/check-go-coverage-ratchet.test.sh
#
# Behavior tests for scripts/check-go-coverage-ratchet.sh — the total-and-
# per-package coverage gate wired into build-test.yml. See #802 for background:
# the total-only 95% floor previously masked pkg/deploy/mcp at 94%, so this
# script now has to fail loudly on either the total OR any listed package
# regressing. That contract had no companion test file until now.

set -euo pipefail

TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

pass() {
  TESTS_RUN=$((TESTS_RUN + 1))
  TESTS_PASSED=$((TESTS_PASSED + 1))
  echo "  ✓ $1"
}

fail() {
  TESTS_RUN=$((TESTS_RUN + 1))
  TESTS_FAILED=$((TESTS_FAILED + 1))
  echo "  ✗ $1"
  echo "    → $2"
}

SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
SUT="${SCRIPT_DIR}/check-go-coverage-ratchet.sh"
# Reuse the sibling script's fixture — pkg/example is 3/3 = 100% and
# pkg/example/sub is 2/4 = 50%, so total coverage is 5/7 = 71.4%.
FIXTURE="${SCRIPT_DIR}/testdata/check-go-package-coverage/sample.coverprofile.txt"

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

echo ""
echo "check-go-coverage-ratchet.sh"
echo ""

# Case 1: total meets floor, no per-package file -> exit 0 with the report.
echo "70" > "$WORK/total.txt"
set +e
OUT=$(bash "$SUT" "$FIXTURE" "$WORK/total.txt" 2>&1)
EXIT=$?
set -e
if [ "$EXIT" -eq 0 ] \
   && echo "$OUT" | grep -q "Total Go coverage: 71.4%" \
   && echo "$OUT" | grep -q "Go coverage ratchet floor: 70%"; then
  pass "passes when total meets the floor and no per-package file is passed"
else
  fail "baseline pass (total-only)" "exit=$EXIT output=$OUT"
fi

# Case 2: total below floor -> exit 1 with a ::error:: annotation.
echo "90" > "$WORK/high.txt"
set +e
OUT=$(bash "$SUT" "$FIXTURE" "$WORK/high.txt" 2>&1)
EXIT=$?
set -e
if [ "$EXIT" -eq 1 ] \
   && echo "$OUT" | grep -q "::error::Go coverage 71.4% is below ratchet floor 90%"; then
  pass "fails with ::error:: when total coverage is below the floor"
else
  fail "total below floor" "exit=$EXIT output=$OUT"
fi

# Case 3: total above floor -> ::notice:: ratchet-up hint.
echo "50" > "$WORK/low.txt"
set +e
OUT=$(bash "$SUT" "$FIXTURE" "$WORK/low.txt" 2>&1)
EXIT=$?
set -e
if [ "$EXIT" -eq 0 ] \
   && echo "$OUT" | grep -q "::notice::Total Go coverage improved above the stored floor"; then
  pass "emits ::notice:: when total coverage exceeds the floor"
else
  fail "total improvement notice" "exit=$EXIT output=$OUT"
fi

# Case 4: per-package file — every package meets its floor -> exit 0.
echo "70" > "$WORK/total-ok.txt"
cat > "$WORK/pkg-ok.txt" <<EOF
# comments and blank lines are ignored

pkg/example 100.0
pkg/example/sub 50.0
EOF
set +e
OUT=$(bash "$SUT" "$FIXTURE" "$WORK/total-ok.txt" "$WORK/pkg-ok.txt" "$WORK/pkg-ok.report.md" 2>&1)
EXIT=$?
set -e
if [ "$EXIT" -eq 0 ] \
   && grep -q "| pkg/example | 100.0% | 100.0% | :white_check_mark: |" "$WORK/pkg-ok.report.md" \
   && grep -q "| pkg/example/sub | 50.0% | 50.0% | :white_check_mark: |" "$WORK/pkg-ok.report.md"; then
  pass "per-package: passes when every listed package meets its floor"
else
  fail "per-package baseline" "exit=$EXIT output=$OUT report=$(cat "$WORK/pkg-ok.report.md" 2>&1)"
fi

# Case 5: per-package regression -> exit 1 with ::error::.
cat > "$WORK/pkg-regress.txt" <<EOF
pkg/example 100.0
pkg/example/sub 90.0
EOF
set +e
OUT=$(bash "$SUT" "$FIXTURE" "$WORK/total-ok.txt" "$WORK/pkg-regress.txt" 2>&1)
EXIT=$?
set -e
if [ "$EXIT" -eq 1 ] \
   && echo "$OUT" | grep -q "::error::Go coverage for pkg/example/sub is 50.0% which is below ratchet floor 90.0%"; then
  pass "per-package: fails with ::error:: when a listed package regresses"
else
  fail "per-package regression" "exit=$EXIT output=$OUT"
fi

# Case 6: per-package listed but not in coverprofile -> exit 1 with ::error::.
cat > "$WORK/pkg-missing.txt" <<EOF
pkg/example 100.0
pkg/does/not/exist 80.0
EOF
set +e
OUT=$(bash "$SUT" "$FIXTURE" "$WORK/total-ok.txt" "$WORK/pkg-missing.txt" 2>&1)
EXIT=$?
set -e
if [ "$EXIT" -eq 1 ] \
   && echo "$OUT" | grep -q "::error::Package pkg/does/not/exist was not found"; then
  pass "per-package: fails when a listed package is absent from the coverprofile"
else
  fail "per-package missing" "exit=$EXIT output=$OUT"
fi

# Case 7: per-package improvement -> ::notice:: ratchet-up hint.
cat > "$WORK/pkg-improve.txt" <<EOF
pkg/example 90.0
pkg/example/sub 50.0
EOF
set +e
OUT=$(bash "$SUT" "$FIXTURE" "$WORK/total-ok.txt" "$WORK/pkg-improve.txt" 2>&1)
EXIT=$?
set -e
if [ "$EXIT" -eq 0 ] \
   && echo "$OUT" | grep -q "::notice::Go coverage for pkg/example improved above the stored floor"; then
  pass "per-package: emits ::notice:: when a package exceeds its floor"
else
  fail "per-package improvement notice" "exit=$EXIT output=$OUT"
fi

# Case 8: sub-package statements are not folded into the parent's scope.
# pkg/example alone is 3/3 = 100%. If sub had been folded in, coverage would
# be 5/7 = 71.4% and the strict 100.0 floor would fail.
cat > "$WORK/pkg-scope.txt" <<EOF
pkg/example 100.0
EOF
set +e
OUT=$(bash "$SUT" "$FIXTURE" "$WORK/total-ok.txt" "$WORK/pkg-scope.txt" "$WORK/scope.report.md" 2>&1)
EXIT=$?
set -e
if [ "$EXIT" -eq 0 ] \
   && grep -q "| pkg/example | 100.0% | 100.0% | :white_check_mark: |" "$WORK/scope.report.md"; then
  pass "per-package: sub-package statements are not folded into the parent scope"
else
  fail "per-package scope isolation" "exit=$EXIT output=$OUT report=$(cat "$WORK/scope.report.md" 2>&1)"
fi

# Case 9: non-numeric total threshold -> exit 1 with an error.
echo "not-a-number" > "$WORK/badtotal.txt"
set +e
OUT=$(bash "$SUT" "$FIXTURE" "$WORK/badtotal.txt" 2>&1)
EXIT=$?
set -e
if [ "$EXIT" -eq 1 ] && echo "$OUT" | grep -q "Coverage threshold must be numeric"; then
  pass "rejects non-numeric total threshold"
else
  fail "bad total threshold" "exit=$EXIT output=$OUT"
fi

# Case 10: non-numeric per-package threshold -> exit 1 with an error.
cat > "$WORK/pkg-badnum.txt" <<EOF
pkg/example not-a-number
EOF
set +e
OUT=$(bash "$SUT" "$FIXTURE" "$WORK/total-ok.txt" "$WORK/pkg-badnum.txt" 2>&1)
EXIT=$?
set -e
if [ "$EXIT" -eq 1 ] && echo "$OUT" | grep -q "Invalid package threshold line"; then
  pass "rejects non-numeric per-package threshold"
else
  fail "bad per-package threshold" "exit=$EXIT output=$OUT"
fi

# Case 11: missing coverprofile -> usage exit 1.
set +e
OUT=$(bash "$SUT" no-such-file.out "$WORK/total-ok.txt" 2>&1)
EXIT=$?
set -e
if [ "$EXIT" -eq 1 ] && echo "$OUT" | grep -q "Coverprofile not found"; then
  pass "missing coverprofile exits with an error"
else
  fail "missing coverprofile" "exit=$EXIT output=$OUT"
fi

# Case 12: missing total-threshold file -> exit 1.
set +e
OUT=$(bash "$SUT" "$FIXTURE" "$WORK/no-such-threshold.txt" 2>&1)
EXIT=$?
set -e
if [ "$EXIT" -eq 1 ] && echo "$OUT" | grep -q "Coverage threshold file not found"; then
  pass "missing total-threshold file exits with an error"
else
  fail "missing total-threshold file" "exit=$EXIT output=$OUT"
fi

# Case 13: missing per-package threshold file (when passed) -> exit 1.
set +e
OUT=$(bash "$SUT" "$FIXTURE" "$WORK/total-ok.txt" "$WORK/no-such-pkg.txt" 2>&1)
EXIT=$?
set -e
if [ "$EXIT" -eq 1 ] && echo "$OUT" | grep -q "Package coverage threshold file not found"; then
  pass "missing per-package threshold file exits with an error"
else
  fail "missing per-package threshold file" "exit=$EXIT output=$OUT"
fi

# Case 14: too few args -> usage.
set +e
OUT=$(bash "$SUT" 2>&1)
EXIT=$?
set -e
if [ "$EXIT" -eq 1 ] && echo "$OUT" | grep -q "Usage:"; then
  pass "no args prints usage"
else
  fail "no args usage" "exit=$EXIT output=$OUT"
fi

# Case 15: too many args -> usage.
set +e
OUT=$(bash "$SUT" a b c d e 2>&1)
EXIT=$?
set -e
if [ "$EXIT" -eq 1 ] && echo "$OUT" | grep -q "Usage:"; then
  pass "extra args print usage"
else
  fail "extra args usage" "exit=$EXIT output=$OUT"
fi

# Case 16: --report-file writes the markdown report to disk.
set +e
OUT=$(bash "$SUT" "$FIXTURE" "$WORK/total-ok.txt" "$WORK/pkg-ok.txt" "$WORK/report.md" 2>&1)
EXIT=$?
set -e
if [ "$EXIT" -eq 0 ] && [ -s "$WORK/report.md" ] \
   && grep -q "## Go coverage ratchet" "$WORK/report.md" \
   && grep -q "Total coverage: \*\*71.4%\*\*" "$WORK/report.md" \
   && grep -q "| pkg/example | 100.0% | 100.0% |" "$WORK/report.md"; then
  pass "writes a markdown report to the --report-file path"
else
  fail "report file" "exit=$EXIT output=$OUT report=$(cat "$WORK/report.md" 2>&1)"
fi

# Case 17: GITHUB_STEP_SUMMARY, if set, is appended to (not overwritten).
: > "$WORK/summary.md"
echo "pre-existing line" > "$WORK/summary.md"
set +e
OUT=$(GITHUB_STEP_SUMMARY="$WORK/summary.md" bash "$SUT" "$FIXTURE" "$WORK/total-ok.txt" 2>&1)
EXIT=$?
set -e
if [ "$EXIT" -eq 0 ] \
   && head -1 "$WORK/summary.md" | grep -q "pre-existing line" \
   && grep -q "## Go coverage ratchet" "$WORK/summary.md"; then
  pass "appends the report to GITHUB_STEP_SUMMARY without truncating existing content"
else
  fail "step-summary append" "exit=$EXIT summary=$(cat "$WORK/summary.md" 2>&1)"
fi

echo ""
echo "──────────────────────────────────────────"
echo "  ${TESTS_PASSED} passed, ${TESTS_FAILED} failed (${TESTS_RUN} total)"
echo "──────────────────────────────────────────"
[ "$TESTS_FAILED" -eq 0 ]
