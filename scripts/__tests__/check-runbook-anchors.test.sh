#!/bin/bash
# scripts/__tests__/check-runbook-anchors.test.sh
#
# Behavior tests for scripts/check-runbook-anchors.py — the runbook-anchor
# gate wired into `make alert-lint` (dashboard-alert-lint.yml). See
# kubestellar-mcp#948 for background: alert descriptions embed deep links
# into runbooks/*.md, and a rename in the runbook would previously ship
# green while breaking every on-call deep-link.

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
SUT="${SCRIPT_DIR}/check-runbook-anchors.py"

if [ ! -f "$SUT" ]; then
  echo "check-runbook-anchors.test.sh: SUT not found at $SUT" >&2
  exit 2
fi

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

setup_case() {
  # $1 = case dir under $WORK
  # Creates <case>/alerts/rules.yaml and <case>/runbooks/ops.md placeholders
  # that individual cases overwrite.
  local d="$WORK/$1"
  mkdir -p "$d/alerts" "$d/runbooks"
  echo "$d"
}

echo ""
echo "check-runbook-anchors.py"
echo ""

# Case 1: every referenced anchor exists (incl. trailing period tolerance,
# GitHub-style slugification with punctuation stripped).
D=$(setup_case case1-all-valid)
cat >"$D/runbooks/ops.md" <<'EOF'
# Runbook

## Diagnosing High Tool Error Rate or Latency
Body.

## Multi-cluster connectivity loss
Body.

## Using the /metrics endpoint
Body.
EOF
cat >"$D/alerts/rules.yaml" <<'EOF'
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
spec:
  groups:
    - name: t
      rules:
        - alert: A1
          annotations:
            description: >-
              See runbooks/ops.md#diagnosing-high-tool-error-rate-or-latency.
        - alert: A2
          annotations:
            description: |-
              Follow runbooks/ops.md#multi-cluster-connectivity-loss,
              then runbooks/ops.md#using-the-metrics-endpoint.
EOF
set +e
OUT=$(python3 "$SUT" --alerts-dir "$D/alerts" --runbooks-dir "$D/runbooks" 2>&1)
EXIT=$?
set -e
if [ "$EXIT" -eq 0 ] \
  && echo "$OUT" | grep -q "3 reference(s) OK" \
  && echo "$OUT" | grep -q "using-the-metrics-endpoint"; then
  pass "Case 1: all anchors resolve — exit 0"
else
  fail "Case 1: all anchors resolve — exit 0" "exit=$EXIT out=$OUT"
fi

# Case 2: alert references an anchor that no longer exists in the runbook —
# exit non-zero, report the miss, still report the OK refs above it.
D=$(setup_case case2-missing-anchor)
cat >"$D/runbooks/ops.md" <<'EOF'
# Runbook

## Renamed heading
Body.
EOF
cat >"$D/alerts/rules.yaml" <<'EOF'
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
spec:
  groups:
    - name: t
      rules:
        - alert: A1
          annotations:
            description: See runbooks/ops.md#diagnosing-silent-failures.
EOF
set +e
OUT=$(python3 "$SUT" --alerts-dir "$D/alerts" --runbooks-dir "$D/runbooks" 2>&1)
EXIT=$?
set -e
if [ "$EXIT" -ne 0 ] \
  && echo "$OUT" | grep -q "MISSING ANCHOR runbooks/ops.md#diagnosing-silent-failures"; then
  pass "Case 2: missing anchor — exit non-zero and reported"
else
  fail "Case 2: missing anchor — exit non-zero and reported" "exit=$EXIT out=$OUT"
fi

# Case 3: alert references a runbook file that does not exist.
D=$(setup_case case3-missing-file)
cat >"$D/runbooks/ops.md" <<'EOF'
# Only file
EOF
cat >"$D/alerts/rules.yaml" <<'EOF'
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
spec:
  groups:
    - name: t
      rules:
        - alert: A
          annotations:
            description: See runbooks/gone.md#anywhere for details.
EOF
set +e
OUT=$(python3 "$SUT" --alerts-dir "$D/alerts" --runbooks-dir "$D/runbooks" 2>&1)
EXIT=$?
set -e
if [ "$EXIT" -ne 0 ] \
  && echo "$OUT" | grep -q "MISSING FILE runbooks/gone.md"; then
  pass "Case 3: missing runbook file — exit non-zero and reported"
else
  fail "Case 3: missing runbook file — exit non-zero and reported" "exit=$EXIT out=$OUT"
fi

# Case 4: no refs at all — exit 0 with a no-op message.
D=$(setup_case case4-no-refs)
cat >"$D/runbooks/ops.md" <<'EOF'
# Runbook
EOF
cat >"$D/alerts/rules.yaml" <<'EOF'
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
spec:
  groups:
    - name: t
      rules:
        - alert: A
          annotations:
            description: Something totally unrelated.
EOF
set +e
OUT=$(python3 "$SUT" --alerts-dir "$D/alerts" --runbooks-dir "$D/runbooks" 2>&1)
EXIT=$?
set -e
if [ "$EXIT" -eq 0 ] && echo "$OUT" | grep -q "no runbook refs found"; then
  pass "Case 4: alert file with no runbook refs — exit 0 no-op"
else
  fail "Case 4: alert file with no runbook refs — exit 0 no-op" "exit=$EXIT out=$OUT"
fi

# Case 5: real repository files still lint clean, so this change does not
# ship a red build against main. Skip if run out-of-tree.
REPO_ALERTS="${SCRIPT_DIR}/../docs/alerts"
REPO_RUNBOOKS="${SCRIPT_DIR}/../runbooks"
if [ -d "$REPO_ALERTS" ] && [ -d "$REPO_RUNBOOKS" ]; then
  set +e
  OUT=$(python3 "$SUT" --alerts-dir "$REPO_ALERTS" --runbooks-dir "$REPO_RUNBOOKS" 2>&1)
  EXIT=$?
  set -e
  if [ "$EXIT" -eq 0 ] && echo "$OUT" | grep -q "reference(s) OK"; then
    pass "Case 5: live docs/alerts + runbooks resolve — exit 0"
  else
    fail "Case 5: live docs/alerts + runbooks resolve — exit 0" "exit=$EXIT out=$OUT"
  fi
fi

echo ""
echo "──────────────────────────────────────────"
echo "check-runbook-anchors.py: $TESTS_PASSED / $TESTS_RUN passed"
echo "──────────────────────────────────────────"

if [ "$TESTS_FAILED" -ne 0 ]; then
  exit 1
fi
