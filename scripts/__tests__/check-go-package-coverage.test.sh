#!/bin/bash
# scripts/__tests__/check-go-package-coverage.test.sh

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
SUT="${SCRIPT_DIR}/check-go-package-coverage.sh"
FIXTURE="${SCRIPT_DIR}/testdata/check-go-package-coverage/sample.coverprofile.txt"

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

echo ""
echo "check-go-package-coverage.sh"
echo ""

# Case 1: both packages meet their floors -> exit 0.
cat > "$WORK/ok.txt" <<EOF
# comment lines and blank lines are ignored

pkg/example 100.0
pkg/example/sub 50.0
EOF
set +e
OUT=$(bash "$SUT" "$FIXTURE" "$WORK/ok.txt" 2>&1)
EXIT=$?
set -e
if [ "$EXIT" -eq 0 ] \
   && echo "$OUT" | grep -q "pkg/example .*100.0%.*100.0%.*ok" \
   && echo "$OUT" | grep -q "pkg/example/sub.*50.0%.*50.0%.*ok"; then
  pass "passes when every package meets its floor"
else
  fail "baseline pass" "exit=$EXIT output=$OUT"
fi

# Case 2: a package below its floor -> exit 1 with ::error::.
cat > "$WORK/fail.txt" <<EOF
pkg/example 100.0
pkg/example/sub 90.0
EOF
set +e
OUT=$(bash "$SUT" "$FIXTURE" "$WORK/fail.txt" 2>&1)
EXIT=$?
set -e
if [ "$EXIT" -eq 1 ] \
   && echo "$OUT" | grep -q "::error::Go coverage for pkg/example/sub is 50.0% which is below ratchet floor 90.0%"; then
  pass "fails and emits ::error:: when a package is below its floor"
else
  fail "regression detection" "exit=$EXIT output=$OUT"
fi

# Case 3: a package listed but not in the coverprofile -> exit 1.
cat > "$WORK/missing.txt" <<EOF
pkg/example 100.0
pkg/does/not/exist 80.0
EOF
set +e
OUT=$(bash "$SUT" "$FIXTURE" "$WORK/missing.txt" 2>&1)
EXIT=$?
set -e
if [ "$EXIT" -eq 1 ] \
   && echo "$OUT" | grep -q "::error::Package pkg/does/not/exist was not found"; then
  pass "fails with ::error:: when a listed package is absent from coverprofile"
else
  fail "missing package" "exit=$EXIT output=$OUT"
fi

# Case 4: improvement above floor -> exit 0 with ::notice::.
cat > "$WORK/improve.txt" <<EOF
pkg/example 90.0
pkg/example/sub 50.0
EOF
set +e
OUT=$(bash "$SUT" "$FIXTURE" "$WORK/improve.txt" 2>&1)
EXIT=$?
set -e
if [ "$EXIT" -eq 0 ] \
   && echo "$OUT" | grep -q "::notice::Go coverage for pkg/example improved to 100.0%"; then
  pass "emits ::notice:: when coverage exceeds the floor"
else
  fail "improvement notice" "exit=$EXIT output=$OUT"
fi

# Case 5: sub-package statements are NOT folded into parent package coverage.
# pkg/example alone is 3/3 = 100%. If sub was folded in, total would be 5/7 = ~71%.
cat > "$WORK/scope.txt" <<EOF
pkg/example 100.0
EOF
set +e
OUT=$(bash "$SUT" "$FIXTURE" "$WORK/scope.txt" 2>&1)
EXIT=$?
set -e
if [ "$EXIT" -eq 0 ] && echo "$OUT" | grep -q "pkg/example .*100.0%.*100.0%.*ok"; then
  pass "sub-package statements are not folded into parent coverage"
else
  fail "scope isolation" "exit=$EXIT output=$OUT"
fi

# Case 6: non-numeric threshold -> exit 1 with ::error::.
cat > "$WORK/badnum.txt" <<EOF
pkg/example not-a-number
EOF
set +e
OUT=$(bash "$SUT" "$FIXTURE" "$WORK/badnum.txt" 2>&1)
EXIT=$?
set -e
if [ "$EXIT" -eq 1 ] && echo "$OUT" | grep -q "::error::Invalid threshold line"; then
  pass "rejects non-numeric threshold with ::error::"
else
  fail "bad threshold" "exit=$EXIT output=$OUT"
fi

# Case 7: missing coverprofile -> usage exit 2.
set +e
OUT=$(bash "$SUT" no-such-file.out "$WORK/ok.txt" 2>&1)
EXIT=$?
set -e
if [ "$EXIT" -eq 2 ]; then
  pass "missing coverprofile returns exit 2"
else
  fail "missing coverprofile" "exit=$EXIT output=$OUT"
fi

echo ""
echo "──────────────────────────────────────────"
echo "  ${TESTS_PASSED} passed, ${TESTS_FAILED} failed (${TESTS_RUN} total)"
echo "──────────────────────────────────────────"
[ "$TESTS_FAILED" -eq 0 ]
