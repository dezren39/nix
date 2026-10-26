#!/usr/bin/env python3
"""Fail when bun.lock and package-lock.json resolve a dependency differently.

This package carries two lockfiles for one package.json (#781):

- package-lock.json (npm) — the only lockfile Renovate updates.
- bun.lock (bun) — the only lockfile the nix build uses
  (flake.nix: `bun install --frozen-lockfile` + `bun x tsc`).

Left unchecked they drift: Renovate bumps landed in the npm lock
only, so the npm path and the shipped bun/nix path compiled
against different dependency versions while both looked green
(@types/vscode sat at 1.140.0 in package-lock.json and 1.110.0 in
bun.lock). Run this after adopting any Renovate PR for this
package — regenerate bun.lock with `bun install` if it fails —
and in CI via the sub-flake check:

    nix flake check pkgs/buffer-backup   # or: python3 check-lock-sync.py

Usage: python3 check-lock-sync.py [package-dir]  (default: this dir)
Exit 0 when every package.json dependency resolves to the same
version in both lockfiles, 1 (with the mismatches listed) otherwise.
"""

import json
import re
import sys
from pathlib import Path


def npm_versions(lock_path: Path) -> dict[str, str]:
    lock = json.loads(lock_path.read_text())
    versions = {}
    for key, entry in lock.get("packages", {}).items():
        if key.startswith("node_modules/") and "version" in entry:
            versions[key.removeprefix("node_modules/")] = entry["version"]
    return versions


def bun_versions(lock_path: Path) -> dict[str, str]:
    # bun.lock is JSON-with-trailing-commas (JSONC); the packages
    # entries are regular enough to read with one pattern:
    #   "name": ["name@<version>", "", {...}, "sha512-..."]
    text = lock_path.read_text()
    versions = {}
    for name, version in re.findall(
        r'"(@?[^"/]+(?:/[^"/]+)?)": \["(?:@?[^"@/]+(?:/[^"@/]+)?)@([^"]+)"', text
    ):
        versions.setdefault(name, version)
    return versions


def main() -> int:
    root = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).parent
    package = json.loads((root / "package.json").read_text())
    deps = {
        **package.get("dependencies", {}),
        **package.get("devDependencies", {}),
    }
    npm = npm_versions(root / "package-lock.json")
    bun = bun_versions(root / "bun.lock")
    problems = []
    for name in sorted(deps):
        npm_v, bun_v = npm.get(name), bun.get(name)
        if npm_v is None or bun_v is None:
            problems.append(
                f"{name}: missing from a lockfile "
                f"(package-lock.json={npm_v}, bun.lock={bun_v})"
            )
        elif npm_v != bun_v:
            problems.append(
                f"{name}: package-lock.json resolves {npm_v}, "
                f"bun.lock resolves {bun_v}"
            )
    if problems:
        print("lockfiles out of sync for pkgs/buffer-backup:", file=sys.stderr)
        for problem in problems:
            print(f"  - {problem}", file=sys.stderr)
        print(
            "Regenerate bun.lock with `bun install` in pkgs/buffer-backup "
            "so both lockfiles resolve the same versions (#781).",
            file=sys.stderr,
        )
        return 1
    print(f"lockfiles in sync: {len(deps)} dependencies agree")
    return 0


if __name__ == "__main__":
    sys.exit(main())
