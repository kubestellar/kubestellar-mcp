#!/bin/bash
# scripts/__tests__/check-go-ratchet-completeness.test.sh
#
# Unit tests for scripts/check-go-ratchet-completeness.sh — the advisory
# lint that reports Go packages missing from the per-package coverage
# ratchet file (kubestellar-mcp#802 step 3).

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
SUT="${SCRIPT_DIR}/check-go-ratchet-completeness.sh"

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

# Build a mini Go tree fixture:
#   fixture/pkg/a/a.go        (non-test, real package)
#   fixture/pkg/b/b.go        (non-test, real package)
#   fixture/pkg/b/b_test.go   (test file only — b is still a package)
#   fixture/pkg/c/c_test.go   (ONLY a test file — should NOT count as a package)
#   fixture/cmd/tool/main.go  (non-test)
#   fixture/pkg/vendor/x/y.go (vendored — must be skipped)
#   fixture/pkg/testdata/d/d.go (testdata — must be skipped)
#   fixture/pkg/a/sub/s.go    (nested package — must be counted independently)
build_fixture() {
  local root="$1"
  mkdir -p "$root/pkg/a/sub" "$root/pkg/b" "$root/pkg/c" \
           "$root/cmd/tool" "$root/pkg/vendor/x" "$root/pkg/testdata/d"
  echo 'package a' > "$root/pkg/a/a.go"
  echo 'package sub' > "$root/pkg/a/sub/s.go"
  echo 'package b' > "$root/pkg/b/b.go"
  echo 'package b' > "$root/pkg/b/b_test.go"
  echo 'package c' > "$root/pkg/c/c_test.go"
  echo 'package main' > "$root/cmd/tool/main.go"
  echo 'package x' > "$root/pkg/vendor/x/y.go"
  echo 'package d' > "$root/pkg/testdata/d/d.go"
}

echo ""
echo "check-go-ratchet-completeness.sh"
echo ""

# ─── Case 1: threshold file lists every real package -> exit 0, silent OK. ───
FIXTURE1="$WORK/case1"
build_fixture "$FIXTURE1"
cat > "$WORK/complete.txt" <<'EOF'
# complete ratchet
pkg/a 100.0
pkg/a/sub 100.0
pkg/b 100.0
cmd/tool 100.0
EOF

OUT=$(cd "$FIXTURE1" && bash "$SUT" "$WORK/complete.txt" pkg cmd 2>&1)
RC=$?
if [ $RC -eq 0 ] && echo "$OUT" | grep -q "All Go packages"; then
  pass "reports OK when every real package is listed"
else
  fail "reports OK when every real package is listed" "exit=$RC out=$OUT"
fi

# ─── Case 2: pkg/b unlisted -> non-strict returns 0 but warns. ───
cat > "$WORK/missing_b.txt" <<'EOF'
pkg/a 100.0
pkg/a/sub 100.0
cmd/tool 100.0
EOF

OUT=$(cd "$FIXTURE1" && bash "$SUT" "$WORK/missing_b.txt" pkg cmd 2>&1) && RC=0 || RC=$?
if [ "$RC" -eq 0 ] && echo "$OUT" | grep -q "::warning::" && echo "$OUT" | grep -q "pkg/b"; then
  pass "warns (exit 0) when a package is missing in non-strict mode"
else
  fail "warns (exit 0) when a package is missing in non-strict mode" "exit=$RC out=$OUT"
fi

# ─── Case 3: same missing pkg/b -> --strict must return non-zero. ───
OUT=$(cd "$FIXTURE1" && bash "$SUT" "$WORK/missing_b.txt" pkg cmd --strict 2>&1) && RC=0 || RC=$?
if [ "$RC" -ne 0 ] && echo "$OUT" | grep -q "::error::"; then
  pass "fails (non-zero exit) when a package is missing in --strict mode"
else
  fail "fails (non-zero exit) when a package is missing in --strict mode" "exit=$RC out=$OUT"
fi

# ─── Case 4: STRICT=1 env var behaves the same as --strict. ───
OUT=$(cd "$FIXTURE1" && STRICT=1 bash "$SUT" "$WORK/missing_b.txt" pkg cmd 2>&1) && RC=0 || RC=$?
if [ "$RC" -ne 0 ] && echo "$OUT" | grep -q "::error::"; then
  pass "STRICT=1 env var enables hard-gate mode"
else
  fail "STRICT=1 env var enables hard-gate mode" "exit=$RC out=$OUT"
fi

# ─── Case 5: pkg/c has ONLY a test file — must NOT be flagged as missing. ───
cat > "$WORK/complete_no_c.txt" <<'EOF'
pkg/a 100.0
pkg/a/sub 100.0
pkg/b 100.0
cmd/tool 100.0
EOF

OUT=$(cd "$FIXTURE1" && bash "$SUT" "$WORK/complete_no_c.txt" pkg cmd 2>&1)
RC=$?
if [ $RC -eq 0 ] && ! echo "$OUT" | grep -q "pkg/c"; then
  pass "test-only directories are not treated as Go packages"
else
  fail "test-only directories are not treated as Go packages" "exit=$RC out=$OUT"
fi

# ─── Case 6: vendor/ subtree must be pruned, even if unlisted. ───
if ! echo "$OUT" | grep -q "vendor"; then
  pass "vendor/ subtree is pruned"
else
  fail "vendor/ subtree is pruned" "vendor mentioned in output: $OUT"
fi

# ─── Case 7: testdata/ subtree must be pruned, even if unlisted. ───
if ! echo "$OUT" | grep -q "testdata"; then
  pass "testdata/ subtree is pruned"
else
  fail "testdata/ subtree is pruned" "testdata mentioned in output: $OUT"
fi

# ─── Case 8: comments and blank lines in threshold file are ignored. ───
cat > "$WORK/commented.txt" <<'EOF'
# top comment

pkg/a 100.0
   # indented comment

pkg/a/sub 100.0
pkg/b 100.0
cmd/tool 100.0
EOF

OUT=$(cd "$FIXTURE1" && bash "$SUT" "$WORK/commented.txt" pkg cmd 2>&1)
RC=$?
if [ $RC -eq 0 ] && echo "$OUT" | grep -q "All Go packages"; then
  pass "blank lines and # comments in threshold file are ignored"
else
  fail "blank lines and # comments in threshold file are ignored" "exit=$RC out=$OUT"
fi

# ─── Case 9: no arguments -> usage error, exit 2. ───
OUT=$(bash "$SUT" 2>&1) && RC=0 || RC=$?
if [ "$RC" -eq 2 ] && echo "$OUT" | grep -q "Usage:"; then
  pass "usage error (exit 2) when no arguments passed"
else
  fail "usage error (exit 2) when no arguments passed" "exit=$RC out=$OUT"
fi

# ─── Case 10: missing threshold file -> exit 2 with clear message. ───
OUT=$(bash "$SUT" "$WORK/does-not-exist.txt" 2>&1) && RC=0 || RC=$?
if [ "$RC" -eq 2 ] && echo "$OUT" | grep -q "not found"; then
  pass "exits 2 with clear message when threshold file is missing"
else
  fail "exits 2 with clear message when threshold file is missing" "exit=$RC out=$OUT"
fi

# ─── Case 11: nonexistent root dir is silently skipped, not an error. ───
cat > "$WORK/only_cmd.txt" <<'EOF'
cmd/tool 100.0
EOF

OUT=$(cd "$FIXTURE1" && bash "$SUT" "$WORK/only_cmd.txt" cmd internal 2>&1)
RC=$?
if [ $RC -eq 0 ]; then
  pass "nonexistent root dir is silently skipped"
else
  fail "nonexistent root dir is silently skipped" "exit=$RC out=$OUT"
fi

# ─── Case 12: default roots pick up pkg + cmd when none are passed. ───
OUT=$(cd "$FIXTURE1" && bash "$SUT" "$WORK/complete.txt" 2>&1)
RC=$?
if [ $RC -eq 0 ] && echo "$OUT" | grep -q "pkg cmd internal"; then
  pass "default roots include pkg, cmd, and internal"
else
  fail "default roots include pkg, cmd, and internal" "exit=$RC out=$OUT"
fi

# ─── Case 13: GITHUB_STEP_SUMMARY is written when set. ───
SUMMARY_FILE="$WORK/summary.md"
: > "$SUMMARY_FILE"
OUT=$(cd "$FIXTURE1" && GITHUB_STEP_SUMMARY="$SUMMARY_FILE" \
      bash "$SUT" "$WORK/missing_b.txt" pkg cmd 2>&1) && RC=0 || RC=$?
if [ "$RC" -eq 0 ] && grep -q "Go ratchet completeness" "$SUMMARY_FILE" && \
   grep -q "pkg/b" "$SUMMARY_FILE"; then
  pass "writes markdown summary to GITHUB_STEP_SUMMARY when set"
else
  fail "writes markdown summary to GITHUB_STEP_SUMMARY when set" "exit=$RC summary=$(cat "$SUMMARY_FILE")"
fi

# ─── Case 14: complete tree writes nothing to GITHUB_STEP_SUMMARY. ───
: > "$SUMMARY_FILE"
OUT=$(cd "$FIXTURE1" && GITHUB_STEP_SUMMARY="$SUMMARY_FILE" \
      bash "$SUT" "$WORK/complete.txt" pkg cmd 2>&1)
RC=$?
if [ $RC -eq 0 ] && [ ! -s "$SUMMARY_FILE" ]; then
  pass "does not write to GITHUB_STEP_SUMMARY when everything is listed"
else
  fail "does not write to GITHUB_STEP_SUMMARY when everything is listed" \
       "exit=$RC summary=$(cat "$SUMMARY_FILE")"
fi

# ─── Case 15: an unknown flag is treated as a root argument (nonexistent -> skip). ───
OUT=$(cd "$FIXTURE1" && bash "$SUT" "$WORK/complete.txt" pkg cmd --unknown-flag 2>&1)
RC=$?
if [ $RC -eq 0 ]; then
  pass "unknown flag is passed through as a (nonexistent) root and skipped"
else
  fail "unknown flag is passed through as a (nonexistent) root and skipped" "exit=$RC out=$OUT"
fi

echo ""
echo "  $TESTS_PASSED / $TESTS_RUN tests passed"
echo ""

if [ "$TESTS_FAILED" -gt 0 ]; then
  exit 1
fi
