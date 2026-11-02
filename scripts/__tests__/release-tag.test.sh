#!/bin/bash
# Guard the GoReleaser tag override: nightly and stable tags may share HEAD.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
python3 - "$ROOT/.github/workflows/release.yml" <<'PY'
import pathlib
import re
import sys

workflow = pathlib.Path(sys.argv[1]).read_text()
# Scope the assertion to the GoReleaser step, not another publisher's env.
step = re.search(
    r"^      - name: Run GoReleaser\n(.*?)(?=^      - name:|\Z)",
    workflow,
    re.MULTILINE | re.DOTALL,
)
assert step, "Run GoReleaser step missing"
assert re.search(
    r"^        env:\n(?:^          .*\n)*"
    r"^          GORELEASER_CURRENT_TAG: \$\{\{ needs\.determine-version\.outputs\.version \}\}$",
    step.group(1),
    re.MULTILINE,
), "GoReleaser must use the calculated version, not auto-select a tag at HEAD"
print("PASS: GoReleaser explicitly uses the calculated release tag")
PY
