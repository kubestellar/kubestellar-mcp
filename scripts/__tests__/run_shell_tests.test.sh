#!/bin/bash
# scripts/__tests__/run_shell_tests.test.sh
#
# Regression tests for scripts/run_shell_tests.sh.

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
SUT="${SCRIPT_DIR}/run_shell_tests.sh"

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

echo ""
echo "──────────────────────────────────────────"
echo "run_shell_tests.sh"
echo "──────────────────────────────────────────"

# -- happy path: two passing tests ------------------------------------------
mkdir -p "$WORK/happy"
cat > "$WORK/happy/a.test.sh" <<'EOF'
#!/bin/bash
echo "a passed"
exit 0
EOF
cat > "$WORK/happy/b.test.sh" <<'EOF'
#!/bin/bash
echo "b passed"
exit 0
EOF
chmod +x "$WORK/happy/"*.test.sh

if out=$(bash "$SUT" "$WORK/happy" 2>&1); then
  if echo "$out" | grep -q "2 / 2 files passed"; then
    pass "happy path: two passing tests → exit 0, aggregate line correct"
  else
    fail "happy path aggregate line" "output: $out"
  fi
else
  fail "happy path exit code" "expected 0, got non-zero. output: $out"
fi

# -- one failing, one passing -----------------------------------------------
mkdir -p "$WORK/mixed"
cat > "$WORK/mixed/pass.test.sh" <<'EOF'
#!/bin/bash
exit 0
EOF
cat > "$WORK/mixed/fail.test.sh" <<'EOF'
#!/bin/bash
echo "boom" >&2
exit 1
EOF
chmod +x "$WORK/mixed/"*.test.sh

if out=$(bash "$SUT" "$WORK/mixed" 2>&1); then
  fail "one failing test should exit non-zero" "got 0. output: $out"
else
  rc=$?
  if [ "$rc" -eq 1 ] && echo "$out" | grep -q "1 of 2 files failed"; then
    pass "mixed pass/fail: exit 1, aggregate line names the failing file"
  else
    fail "mixed exit code / aggregate" "exit=$rc output: $out"
  fi
fi

# -- missing directory returns exit 2 ---------------------------------------
if out=$(bash "$SUT" "$WORK/does-not-exist" 2>&1); then
  fail "missing dir should be exit 2" "got 0. output: $out"
else
  rc=$?
  if [ "$rc" -eq 2 ] && echo "$out" | grep -q "test directory not found"; then
    pass "missing directory → exit 2 with clear message"
  else
    fail "missing dir exit code" "exit=$rc output: $out"
  fi
fi

# -- empty directory (no *.test.sh) returns exit 2 --------------------------
mkdir -p "$WORK/empty"
# Put a non-matching file to prove the filter is *.test.sh, not *
touch "$WORK/empty/README.md"

if out=$(bash "$SUT" "$WORK/empty" 2>&1); then
  fail "empty dir should be exit 2" "got 0. output: $out"
else
  rc=$?
  if [ "$rc" -eq 2 ] && echo "$out" | grep -q "no \\*.test.sh files found"; then
    pass "empty directory → exit 2 with clear message"
  else
    fail "empty dir exit code" "exit=$rc output: $out"
  fi
fi

# -- default test dir is scripts/__tests__ ----------------------------------
# Running with no args would recurse into this test file itself, so instead
# we prove the default resolution by copying the runner to a scratch bin/
# whose sibling __tests__ dir contains one trivial test. If the default
# resolves relative to the runner's own dir, that test runs and passes.
mkdir -p "$WORK/default/bin" "$WORK/default/bin/__tests__"
cp "$SUT" "$WORK/default/bin/run_shell_tests.sh"
chmod +x "$WORK/default/bin/run_shell_tests.sh"
cat > "$WORK/default/bin/__tests__/sentinel.test.sh" <<'EOF'
#!/bin/bash
echo "SENTINEL=hit"
exit 0
EOF
chmod +x "$WORK/default/bin/__tests__/sentinel.test.sh"

if out=$(bash "$WORK/default/bin/run_shell_tests.sh" 2>&1); then
  if echo "$out" | grep -q "SENTINEL=hit"; then
    pass "no-argument invocation defaults to \$SCRIPT_DIR/__tests__"
  else
    fail "default dir resolution" "output: $out"
  fi
else
  rc=$?
  fail "no-argument invocation" "expected 0, got $rc. output: $out"
fi

# -- deterministic ordering (alphabetical) ----------------------------------
mkdir -p "$WORK/order"
cat > "$WORK/order/z_last.test.sh"  <<'EOF'
#!/bin/bash
echo "MARKER=z"
exit 0
EOF
cat > "$WORK/order/a_first.test.sh" <<'EOF'
#!/bin/bash
echo "MARKER=a"
exit 0
EOF
chmod +x "$WORK/order/"*.test.sh
out=$(bash "$SUT" "$WORK/order" 2>&1)
first=$(echo "$out" | grep -oE 'MARKER=[az]' | head -1)
second=$(echo "$out" | grep -oE 'MARKER=[az]' | tail -1)
if [ "$first" = "MARKER=a" ] && [ "$second" = "MARKER=z" ]; then
  pass "tests run in deterministic alphabetical order"
else
  fail "test ordering" "first=$first second=$second"
fi

# -- non-*.test.sh files are ignored ----------------------------------------
mkdir -p "$WORK/filter"
cat > "$WORK/filter/keep.test.sh" <<'EOF'
#!/bin/bash
echo "MARKER=keep"
exit 0
EOF
cat > "$WORK/filter/skip.sh" <<'EOF'
#!/bin/bash
echo "MARKER=skip-me-please"
exit 1
EOF
chmod +x "$WORK/filter/"*.sh
if out=$(bash "$SUT" "$WORK/filter" 2>&1); then
  if echo "$out" | grep -q "MARKER=keep" && ! echo "$out" | grep -q "MARKER=skip-me-please"; then
    pass "only files ending in .test.sh are discovered"
  else
    fail "filter behavior" "output: $out"
  fi
else
  rc=$?
  fail "filter run" "exit=$rc output: $out"
fi

echo ""
echo "──────────────────────────────────────────"
if [ "$TESTS_FAILED" -eq 0 ]; then
  echo "  $TESTS_PASSED passed, 0 failed ($TESTS_RUN total)"
  echo "──────────────────────────────────────────"
  exit 0
fi
echo "  $TESTS_PASSED passed, $TESTS_FAILED failed ($TESTS_RUN total)"
echo "──────────────────────────────────────────"
exit 1
