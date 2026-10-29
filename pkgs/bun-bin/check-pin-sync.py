#!/usr/bin/env python3
"""Fail when the two bun pins in this repo disagree.

Two places pin the bun version and they must stay in step (#1091):

- .github/workflows/buffer-backup-renovate-bun-sync.yml (`bun-version`)
  — the bun that regenerates pkgs/buffer-backup's bun.lock.
- pkgs/bun-bin/hashes.json (`version`) — the bun this repo installs;
  pkgs/bun-bin/package.nix fetchurls bun-v<version> using the four
  platform hashes in the same file, so the version cannot be bumped
  without the hashes.

Renovate bumps the workflow pin alone (it has no manager that can
see hashes.json, and a version-only bump of hashes.json would break
the fetch hashes), so until this check existed the pins were kept in
step by a code comment and a human noticing — they already split
once (#1089, on PR #1064). This check is the enforcing half: CI runs
it via the bun-pin-sync job in
.github/workflows/buffer-backup-lock-sync.yml whenever either pin
(or this script) changes.

Usage: python3 pkgs/bun-bin/check-pin-sync.py [--repo-root DIR]
Exit 0 when the pins agree, 1 (naming both pins and versions)
otherwise.
"""

import json
import re
import sys
from pathlib import Path

WORKFLOW = Path(".github/workflows/buffer-backup-renovate-bun-sync.yml")
HASHES = Path("pkgs/bun-bin/hashes.json")


def workflow_bun_version(root: Path) -> str | None:
    text = (root / WORKFLOW).read_text()
    match = re.search(r'^\s*bun-version:\s*"?([^"\s]+)"?\s*$', text, re.MULTILINE)
    return match.group(1) if match else None


def hashes_bun_version(root: Path) -> str | None:
    data = json.loads((root / HASHES).read_text())
    return data.get("version")


def main() -> int:
    args = sys.argv[1:]
    if args[:1] == ["--repo-root"] and len(args) == 2:
        root = Path(args[1])
    elif args:
        print(f"usage: {sys.argv[0]} [--repo-root DIR]", file=sys.stderr)
        return 2
    else:
        # pkgs/bun-bin/check-pin-sync.py -> repo root is two levels up.
        root = Path(__file__).resolve().parents[2]

    workflow_version = workflow_bun_version(root)
    hashes_version = hashes_bun_version(root)

    if workflow_version is None or hashes_version is None:
        print("bun pins out of sync: could not read both pins:", file=sys.stderr)
        print(
            f"  - {WORKFLOW} bun-version: {workflow_version}",
            file=sys.stderr,
        )
        print(f"  - {HASHES} version: {hashes_version}", file=sys.stderr)
        return 1

    if workflow_version != hashes_version:
        print("bun pins out of sync:", file=sys.stderr)
        print(
            f"  - {WORKFLOW} bun-version is {workflow_version}",
            file=sys.stderr,
        )
        print(f"  - {HASHES} version is {hashes_version}", file=sys.stderr)
        print(
            "Bump both pins together: the workflow bun regenerates "
            "pkgs/buffer-backup's bun.lock and must be the bun pinned "
            "in pkgs/bun-bin (#1091).",
            file=sys.stderr,
        )
        return 1

    print(
        f"bun pins in sync: {WORKFLOW} bun-version and "
        f"{HASHES} version are both {workflow_version}"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
