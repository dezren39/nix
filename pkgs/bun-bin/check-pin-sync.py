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
Exit 0 when the pins agree, 1 (naming both pins and versions) when
they disagree, 2 for an invocation error — a bad argument, or a pin
file missing, unreadable, or malformed — so a broken checkout or a
corrupt edit can never be mistaken for drift by a caller branching
on exit 1 (#1148; the #1141 convention). Invocation errors are one
line on stderr naming the offending file, never a traceback.
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
    if not isinstance(data, dict):
        raise ValueError(f"expected a JSON object, got {type(data).__name__}")
    version = data.get("version")
    # Scalar shape (#1204): a present-but-non-string version (a number,
    # list, object or bool from a hand edit or bad merge) is malformed
    # input, not drift -- main() maps this ValueError to exit 2, the
    # same path as the container-shape check above. A missing/null
    # version keeps returning None (the could-not-read branch, exit 1).
    if version is not None and not isinstance(version, str):
        raise ValueError(
            f"'version' must be a string, got {type(version).__name__}"
        )
    return version


def invocation_error(message: str) -> int:
    print(f"check-pin-sync: error: {message}", file=sys.stderr)
    return 2


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

    # Guarded reads (#1148): a missing or unreadable pin file is an
    # invocation error (exit 2, one line naming the file), not a
    # traceback at exit 1 where callers read "pins out of sync".
    try:
        workflow_version = workflow_bun_version(root)
    except OSError as exc:
        return invocation_error(f"{root / WORKFLOW}: {exc.strerror or exc}")
    except UnicodeDecodeError as exc:
        # read_text() decodes UTF-8 strictly: a corrupt/binary-damaged
        # workflow file is an invocation error (#1189), not drift.
        # UnicodeDecodeError is a ValueError, not an OSError, so the
        # guard above does not catch it (the hashes read below is
        # covered only because its ValueError branch catches it).
        return invocation_error(f"{root / WORKFLOW}: not valid UTF-8: {exc}")
    try:
        hashes_version = hashes_bun_version(root)
    except OSError as exc:
        return invocation_error(f"{root / HASHES}: {exc.strerror or exc}")
    except (json.JSONDecodeError, ValueError) as exc:
        return invocation_error(f"{root / HASHES}: invalid JSON: {exc}")

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
