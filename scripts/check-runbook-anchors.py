#!/usr/bin/env python3
"""
scripts/check-runbook-anchors.py

Verify every ``runbooks/<file>.md#<anchor>`` reference embedded in
``docs/alerts/*.yaml`` alert annotations resolves to a real ATX heading in the
runbook. Wired into ``make alert-lint`` so a runbook rename or an alert typo
fails CI pre-merge instead of silently breaking the on-call deep-link
experience — see kubestellar-mcp#948 for background.

Usage:
    scripts/check-runbook-anchors.py [--alerts-dir DIR] [--runbooks-dir DIR]

Defaults:
    --alerts-dir    docs/alerts
    --runbooks-dir  runbooks

Exit codes:
    0 — every referenced anchor resolves.
    1 — at least one referenced anchor is missing or the file is missing.
    2 — usage error (no alert files, missing dirs).
"""

from __future__ import annotations

import argparse
import os
import re
import sys
from typing import Iterable

try:
    import yaml
except ImportError:
    sys.stderr.write(
        "check-runbook-anchors.py: PyYAML is required "
        "(already a build-test dependency).\n"
    )
    sys.exit(2)


# Match runbooks/<file>.md#<anchor>. The anchor stops at the first character
# that GitHub's slugifier could not emit — that also strips any trailing
# sentence punctuation the reference is embedded in (period, comma, ')', etc).
REF_RE = re.compile(r"runbooks/([A-Za-z0-9._/-]+\.md)#([A-Za-z0-9-]+)")


def slugify_heading(text: str) -> str:
    """Approximate GitHub's Markdown heading slugifier.

    Rules used in practice:
    - Lower-case.
    - Drop everything that is not alphanumeric, hyphen, underscore, or space.
    - Collapse whitespace runs to a single hyphen.
    """
    text = text.strip().lower()
    # Drop characters GitHub drops (punctuation etc).
    text = re.sub(r"[^\w\s-]", "", text, flags=re.UNICODE)
    # Whitespace -> hyphen.
    text = re.sub(r"\s+", "-", text)
    # Collapse repeated hyphens.
    text = re.sub(r"-+", "-", text)
    return text.strip("-")


def extract_anchors(md_path: str) -> set[str]:
    """Return the set of anchor slugs defined by ATX headings in ``md_path``."""
    anchors: set[str] = set()
    with open(md_path, "r", encoding="utf-8") as fh:
        in_fence = False
        for line in fh:
            stripped = line.rstrip("\n")
            # Skip fenced code blocks so a "# comment" inside a shell block
            # is not mistaken for a heading.
            if stripped.lstrip().startswith("```"):
                in_fence = not in_fence
                continue
            if in_fence:
                continue
            m = re.match(r"^(#{1,6})\s+(.+?)\s*#*\s*$", stripped)
            if not m:
                continue
            anchors.add(slugify_heading(m.group(2)))
    return anchors


def walk_strings(value) -> Iterable[str]:
    """Yield every string leaf from a nested YAML structure."""
    if isinstance(value, str):
        yield value
    elif isinstance(value, dict):
        for v in value.values():
            yield from walk_strings(v)
    elif isinstance(value, list):
        for v in value:
            yield from walk_strings(v)


def collect_refs(alerts_dir: str) -> list[tuple[str, str, str]]:
    """Return (alert_file, runbook_relpath, anchor) for every ref found."""
    refs: list[tuple[str, str, str]] = []
    if not os.path.isdir(alerts_dir):
        sys.stderr.write(f"check-runbook-anchors.py: alerts dir not found: {alerts_dir}\n")
        sys.exit(2)
    files = sorted(
        os.path.join(alerts_dir, f)
        for f in os.listdir(alerts_dir)
        if f.endswith((".yaml", ".yml"))
    )
    if not files:
        sys.stderr.write(f"check-runbook-anchors.py: no *.yaml files under {alerts_dir}\n")
        sys.exit(2)
    for path in files:
        with open(path, "r", encoding="utf-8") as fh:
            try:
                doc = yaml.safe_load(fh)
            except yaml.YAMLError as exc:
                sys.stderr.write(f"{path}: invalid YAML: {exc}\n")
                sys.exit(1)
        for text in walk_strings(doc):
            for m in REF_RE.finditer(text):
                refs.append((path, m.group(1), m.group(2)))
    return refs


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--alerts-dir", default="docs/alerts")
    ap.add_argument("--runbooks-dir", default="runbooks")
    args = ap.parse_args()

    refs = collect_refs(args.alerts_dir)
    if not refs:
        print("check-runbook-anchors.py: no runbook refs found — nothing to check.")
        return 0

    # Cache extracted anchors per runbook file.
    anchors_by_file: dict[str, set[str] | None] = {}

    def anchors_for(rb_rel: str) -> set[str] | None:
        if rb_rel in anchors_by_file:
            return anchors_by_file[rb_rel]
        full = os.path.join(args.runbooks_dir, rb_rel)
        if not os.path.isfile(full):
            anchors_by_file[rb_rel] = None
            return None
        anchors_by_file[rb_rel] = extract_anchors(full)
        return anchors_by_file[rb_rel]

    fails: list[str] = []
    for alert_path, rb_rel, anchor in refs:
        anchors = anchors_for(rb_rel)
        if anchors is None:
            fails.append(
                f"MISSING FILE runbooks/{rb_rel} (referenced by {alert_path} as #{anchor})"
            )
            continue
        if anchor not in anchors:
            fails.append(
                f"MISSING ANCHOR runbooks/{rb_rel}#{anchor} (referenced by {alert_path})"
            )
            continue
        print(f"OK runbooks/{rb_rel}#{anchor}")

    if fails:
        print()
        for f in fails:
            print(f)
        print(f"\ncheck-runbook-anchors.py: {len(fails)} bad reference(s)")
        return 1
    print(f"\ncheck-runbook-anchors.py: {len(refs)} reference(s) OK")
    return 0


if __name__ == "__main__":
    sys.exit(main())
