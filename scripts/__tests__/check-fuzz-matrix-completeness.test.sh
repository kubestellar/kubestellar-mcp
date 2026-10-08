#!/bin/bash
# scripts/__tests__/check-fuzz-matrix-completeness.test.sh
#
# Unit tests for scripts/check-fuzz-matrix-completeness.sh — the advisory
# lint that reports Go fuzz targets missing from .github/workflows/fuzz.yml's
# matrix (kubestellar-mcp#1169 follow-up: prevents the matrix from drifting
# out of sync with the repo's actual FuzzXxx functions again).

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
SUT="${SCRIPT_DIR}/check-fuzz-matrix-completeness.sh"

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

# Build a mini Go tree fixture:
#   fixture/pkg/a/fuzz_test.go      (FuzzA — real fuzz target)
#   fixture/pkg/b/fuzz_test.go      (FuzzB — real fuzz target)
#   fixture/pkg/b/other_test.go     (non-fuzz test — must be ignored)
#   fixture/pkg/vendor/x/fuzz_test.go (vendored — must be skipped)
#   fixture/pkg/testdata/d/fuzz_test.go (testdata — must be skipped)
#   fixture/cmd/tool/fuzz_test.go   (FuzzTool — nested root)
build_fixture() {
  local root="$1"
  mkdir -p "$root/pkg/a" "$root/pkg/b" "$root/pkg/vendor/x" \
           "$root/pkg/testdata/d" "$root/cmd/tool"
  cat > "$root/pkg/a/fuzz_test.go" <<'EOF'
package a

import "testing"

func FuzzA(f *testing.F) {
	f.Fuzz(func(t *testing.T, s string) {})
}
EOF
  cat > "$root/pkg/b/fuzz_test.go" <<'EOF'
package b

import "testing"

func FuzzB(f *testing.F) {
	f.Fuzz(func(t *testing.T, s string) {})
}
EOF
  cat > "$root/pkg/b/other_test.go" <<'EOF'
package b

import "testing"

func TestNotAFuzzTarget(t *testing.T) {}
EOF
  cat > "$root/pkg/vendor/x/fuzz_test.go" <<'EOF'
package x

import "testing"

func FuzzVendored(f *testing.F) {}
EOF
  cat > "$root/pkg/testdata/d/fuzz_test.go" <<'EOF'
package d

import "testing"

func FuzzTestdata(f *testing.F) {}
EOF
  cat > "$root/cmd/tool/fuzz_test.go" <<'EOF'
package main

import "testing"

func FuzzTool(f *testing.F) {
	f.Fuzz(func(t *testing.T, s string) {})
}
EOF
}

echo ""
echo "check-fuzz-matrix-completeness.sh"
echo ""

# ─── Case 1: workflow lists every real target -> exit 0, silent OK. ───
FIXTURE1="$WORK/case1"
build_fixture "$FIXTURE1"
cat > "$WORK/complete.yml" <<'EOF'
jobs:
  fuzz:
    strategy:
      matrix:
        include:
          - package: ./pkg/a/
            target: FuzzA
          - package: ./pkg/b/
            target: FuzzB
          - package: ./cmd/tool/
            target: FuzzTool
EOF

OUT=$(cd "$FIXTURE1" && bash "$SUT" "$WORK/complete.yml" pkg cmd 2>&1)
RC=$?
if [ $RC -eq 0 ] && echo "$OUT" | grep -q "All Go fuzz targets"; then
  pass "reports OK when every real target is listed"
else
  fail "reports OK when every real target is listed" "exit=$RC out=$OUT"
fi

# ─── Case 2: FuzzB unlisted -> non-strict returns 0 but warns. ───
cat > "$WORK/missing_b.yml" <<'EOF'
jobs:
  fuzz:
    strategy:
      matrix:
        include:
          - package: ./pkg/a/
            target: FuzzA
          - package: ./cmd/tool/
            target: FuzzTool
EOF

OUT=$(cd "$FIXTURE1" && bash "$SUT" "$WORK/missing_b.yml" pkg cmd 2>&1) && RC=0 || RC=$?
if [ "$RC" -eq 0 ] && echo "$OUT" | grep -q "::warning::" && echo "$OUT" | grep -q "FuzzB"; then
  pass "warns (exit 0) when a target is missing in non-strict mode"
else
  fail "warns (exit 0) when a target is missing in non-strict mode" "exit=$RC out=$OUT"
fi

# ─── Case 3: same missing FuzzB -> --strict must return non-zero. ───
OUT=$(cd "$FIXTURE1" && bash "$SUT" "$WORK/missing_b.yml" pkg cmd --strict 2>&1) && RC=0 || RC=$?
if [ "$RC" -ne 0 ] && echo "$OUT" | grep -q "::error::"; then
  pass "fails (non-zero exit) when a target is missing in --strict mode"
else
  fail "fails (non-zero exit) when a target is missing in --strict mode" "exit=$RC out=$OUT"
fi

# ─── Case 4: STRICT=1 env var behaves the same as --strict. ───
OUT=$(cd "$FIXTURE1" && STRICT=1 bash "$SUT" "$WORK/missing_b.yml" pkg cmd 2>&1) && RC=0 || RC=$?
if [ "$RC" -ne 0 ] && echo "$OUT" | grep -q "::error::"; then
  pass "STRICT=1 env var enables hard-gate mode"
else
  fail "STRICT=1 env var enables hard-gate mode" "exit=$RC out=$OUT"
fi

# ─── Case 5: a non-fuzz test func must NOT be reported as a target. ───
if ! echo "$OUT" | grep -q "TestNotAFuzzTarget"; then
  pass "non-fuzz test functions are not treated as fuzz targets"
else
  fail "non-fuzz test functions are not treated as fuzz targets" "unexpected mention: $OUT"
fi

# ─── Case 6: vendor/ subtree must be pruned, even if unlisted. ───
if ! echo "$OUT" | grep -q "FuzzVendored"; then
  pass "vendor/ subtree is pruned"
else
  fail "vendor/ subtree is pruned" "vendor target mentioned in output: $OUT"
fi

# ─── Case 7: testdata/ subtree must be pruned, even if unlisted. ───
if ! echo "$OUT" | grep -q "FuzzTestdata"; then
  pass "testdata/ subtree is pruned"
else
  fail "testdata/ subtree is pruned" "testdata target mentioned in output: $OUT"
fi

# ─── Case 8: no arguments -> usage error, exit 2. ───
OUT=$(bash "$SUT" 2>&1) && RC=0 || RC=$?
if [ "$RC" -eq 2 ] && echo "$OUT" | grep -q "Usage:"; then
  pass "usage error (exit 2) when no arguments passed"
else
  fail "usage error (exit 2) when no arguments passed" "exit=$RC out=$OUT"
fi

# ─── Case 9: missing workflow file -> exit 2 with clear message. ───
OUT=$(bash "$SUT" "$WORK/does-not-exist.yml" 2>&1) && RC=0 || RC=$?
if [ "$RC" -eq 2 ] && echo "$OUT" | grep -q "not found"; then
  pass "exits 2 with clear message when workflow file is missing"
else
  fail "exits 2 with clear message when workflow file is missing" "exit=$RC out=$OUT"
fi

# ─── Case 10: nonexistent root dir is silently skipped, not an error. ───
cat > "$WORK/only_cmd.yml" <<'EOF'
jobs:
  fuzz:
    strategy:
      matrix:
        include:
          - package: ./cmd/tool/
            target: FuzzTool
EOF

OUT=$(cd "$FIXTURE1" && bash "$SUT" "$WORK/only_cmd.yml" cmd internal 2>&1)
RC=$?
if [ $RC -eq 0 ]; then
  pass "nonexistent root dir is silently skipped"
else
  fail "nonexistent root dir is silently skipped" "exit=$RC out=$OUT"
fi

# ─── Case 11: default roots pick up pkg + cmd + internal when none passed. ───
OUT=$(cd "$FIXTURE1" && bash "$SUT" "$WORK/complete.yml" 2>&1)
RC=$?
if [ $RC -eq 0 ] && echo "$OUT" | grep -q "All Go fuzz targets"; then
  pass "default roots (pkg, cmd, internal) find every target"
else
  fail "default roots (pkg, cmd, internal) find every target" "exit=$RC out=$OUT"
fi

# ─── Case 12: GITHUB_STEP_SUMMARY is written when set. ───
SUMMARY_FILE="$WORK/summary.md"
: > "$SUMMARY_FILE"
OUT=$(cd "$FIXTURE1" && GITHUB_STEP_SUMMARY="$SUMMARY_FILE" \
      bash "$SUT" "$WORK/missing_b.yml" pkg cmd 2>&1) && RC=0 || RC=$?
if [ "$RC" -eq 0 ] && grep -q "Fuzz matrix completeness" "$SUMMARY_FILE" && \
   grep -q "FuzzB" "$SUMMARY_FILE"; then
  pass "writes markdown summary to GITHUB_STEP_SUMMARY when set"
else
  fail "writes markdown summary to GITHUB_STEP_SUMMARY when set" "exit=$RC summary=$(cat "$SUMMARY_FILE")"
fi

# ─── Case 13: complete tree writes nothing to GITHUB_STEP_SUMMARY. ───
: > "$SUMMARY_FILE"
OUT=$(cd "$FIXTURE1" && GITHUB_STEP_SUMMARY="$SUMMARY_FILE" \
      bash "$SUT" "$WORK/complete.yml" pkg cmd 2>&1)
RC=$?
if [ $RC -eq 0 ] && [ ! -s "$SUMMARY_FILE" ]; then
  pass "does not write to GITHUB_STEP_SUMMARY when everything is listed"
else
  fail "does not write to GITHUB_STEP_SUMMARY when everything is listed" \
       "exit=$RC summary=$(cat "$SUMMARY_FILE")"
fi

# ─── Case 14: the real repo's fuzz.yml is a regression-detectable fixture:
#     deliberately point at this repo's real workflow + source tree and
#     confirm the script runs cleanly (exit 0 or 1, never a crash/exit 2).
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
if [ -f "$REPO_ROOT/.github/workflows/fuzz.yml" ]; then
  set +e
  (cd "$REPO_ROOT" && bash "$SUT" .github/workflows/fuzz.yml pkg cmd internal >/dev/null 2>&1)
  REAL_RC=$?
  set -e
  if [ "$REAL_RC" -eq 0 ] || [ "$REAL_RC" -eq 1 ]; then
    pass "runs cleanly against the real repo's fuzz.yml (exit $REAL_RC)"
  else
    fail "runs cleanly against the real repo's fuzz.yml" "unexpected exit=$REAL_RC"
  fi
else
  pass "skipped real-repo check (fuzz.yml not found outside repo checkout)"
fi

echo ""
echo "  $TESTS_PASSED / $TESTS_RUN tests passed"
echo ""

if [ "$TESTS_FAILED" -gt 0 ]; then
  exit 1
fi
