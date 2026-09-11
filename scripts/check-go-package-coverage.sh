#!/bin/bash
# scripts/check-go-package-coverage.sh
#
# Enforces per-package Go coverage floors against a generated coverprofile.
# Coverage may rise above the stored floors, but must never drop below them.
#
# Complements the total-coverage check in .github/workflows/build-test.yml,
# which reads a single aggregate floor from .github/go-coverage-ratchet.txt.
# Without a per-package floor, well-covered packages mask regressions in
# larger, less-covered packages (see kubestellar-mcp#802 — pkg/deploy/mcp
# at 94% was silently masked by the 95% total floor).
#
# Usage:
#   ./scripts/check-go-package-coverage.sh <coverprofile> <package-threshold-file>

set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "Usage: $0 <coverprofile> <package-threshold-file>" >&2
  exit 2
fi

COVERPROFILE="$1"
THRESHOLD_FILE="$2"
MODULE_PREFIX="${GO_MODULE_PREFIX:-github.com/kubestellar/kubestellar-mcp/}"
FAILURES=0

if [ ! -f "$COVERPROFILE" ]; then
  echo "Coverprofile not found: $COVERPROFILE" >&2
  exit 2
fi

if [ ! -f "$THRESHOLD_FILE" ]; then
  echo "Package coverage threshold file not found: $THRESHOLD_FILE" >&2
  exit 2
fi

is_number() {
  [[ "$1" =~ ^[0-9]+([.][0-9]+)?$ ]]
}

is_less_than() {
  awk -v left="$1" -v right="$2" 'BEGIN { exit !(left + 0 < right + 0) }'
}

is_greater_than() {
  awk -v left="$1" -v right="$2" 'BEGIN { exit !(left + 0 > right + 0) }'
}

# coverage_from_profile <package-path>
# Computes covered/total statement ratio from lines in <coverprofile> whose
# file path (after stripping MODULE_PREFIX) is a direct child of
# <package-path> — i.e., the package_dir equals <package-path> exactly. Lines
# in sub-packages are not folded in.
coverage_from_profile() {
  local package_path="$1"
  awk -v module_prefix="$MODULE_PREFIX" -v target="$package_path" '
    BEGIN { FS = "[: ,]+" }
    NR == 1 { next }
    {
      file = $1
      sub("^" module_prefix, "", file)
      depth = split(file, parts, "/")
      package_dir = parts[1]
      for (segment_index = 2; segment_index < depth; segment_index++) {
        package_dir = package_dir "/" parts[segment_index]
      }
      if (package_dir != target) { next }
      statements = $(NF - 1)
      executions = $NF
      total += statements
      if (executions > 0) { covered += statements }
    }
    END {
      if (total == 0) { exit 1 }
      printf "%.1f", (covered / total) * 100
    }
  ' "$COVERPROFILE"
}

printf 'Per-package Go coverage ratchet\n'
printf '%-40s %10s %10s %s\n' 'Package' 'Current' 'Required' 'Status'

while read -r package_path minimum_coverage _rest; do
  if [ -z "${package_path:-}" ] || [[ "$package_path" == \#* ]]; then
    continue
  fi

  if [ -z "${minimum_coverage:-}" ] || ! is_number "$minimum_coverage"; then
    echo "::error::Invalid threshold line in ${THRESHOLD_FILE}: ${package_path} ${minimum_coverage:-}"
    FAILURES=1
    continue
  fi

  if ! current_coverage=$(coverage_from_profile "$package_path"); then
    echo "::error::Package ${package_path} was not found in ${COVERPROFILE}"
    printf '%-40s %10s %9s%% %s\n' "$package_path" 'missing' "$minimum_coverage" 'FAIL'
    FAILURES=1
    continue
  fi

  status='ok'
  if is_less_than "$current_coverage" "$minimum_coverage"; then
    status='FAIL'
    FAILURES=1
    echo "::error::Go coverage for ${package_path} is ${current_coverage}% which is below ratchet floor ${minimum_coverage}%"
  elif is_greater_than "$current_coverage" "$minimum_coverage"; then
    echo "::notice::Go coverage for ${package_path} improved to ${current_coverage}%; update ${THRESHOLD_FILE} to lock it in."
  fi

  printf '%-40s %9s%% %9s%% %s\n' "$package_path" "$current_coverage" "$minimum_coverage" "$status"
done < "$THRESHOLD_FILE"

if [ "$FAILURES" -ne 0 ]; then
  exit 1
fi
