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

Usage: python3 check-lock-sync.py [--help] [package-dir]  (default: this dir)
Exit 0 when every package.json dependency resolves to the same
version in both lockfiles, 1 (with the mismatches listed) when the
lockfiles disagree, 2 for an invocation error — an unknown or extra
argument, or a package file missing/unreadable/wrong-shaped — so a mistyped call
can never be mistaken for drift by a caller branching on exit 1
(#1141; the #1133 shape). Invocation errors are one line on stderr
naming the offending argument or file, never a traceback.
"""

import json
import re
import sys
from pathlib import Path

USAGE = "usage: check-lock-sync.py [--help] [package-dir]"


def npm_versions(lock_path: Path) -> dict[str, str]:
    # Shape validation (#1159): a file that parses as JSON but has the
    # wrong shape (a list at the top level, a non-object "packages", a
    # non-object entry) used to raise AttributeError/TypeError here and
    # traceback at exit 1 -- the "lockfiles disagree" signal. Raise
    # ValueError naming the file instead; main() maps it to exit 2.
    lock = json.loads(lock_path.read_text())
    if not isinstance(lock, dict):
        raise ValueError(f"{lock_path}: expected a JSON object")
    packages = lock.get("packages", {})
    if not isinstance(packages, dict):
        raise ValueError(f"{lock_path}: 'packages' must be an object")
    versions = {}
    for key, entry in packages.items():
        if not isinstance(entry, dict):
            raise ValueError(
                f"{lock_path}: 'packages' entry '{key}' must be an object"
            )
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


def invocation_error(message: str) -> int:
    print(f"check-lock-sync: error: {message}", file=sys.stderr)
    return 2


def parse_root(argv: list[str]) -> Path | int:
    """Validate argv before any file is read (#1141).

    Returns the package dir, or an exit-2 code after naming the
    offending argument on stderr. --help as the sole argument prints
    usage on stdout and returns exit 0.
    """
    args = argv[1:]
    if args in (["--help"], ["-h"]):
        print(USAGE)
        print("Exit 0 = lockfiles in sync, 1 = out of sync, 2 = invocation error.")
        return 0
    for arg in args:
        if arg.startswith("-"):
            print(f"check-lock-sync: error: unknown argument: {arg}", file=sys.stderr)
            print(USAGE, file=sys.stderr)
            return 2
        if arg == "":
            print("check-lock-sync: error: empty package-dir argument", file=sys.stderr)
            print(USAGE, file=sys.stderr)
            return 2
    if len(args) > 1:
        print(
            f"check-lock-sync: error: unexpected extra argument: {args[1]}",
            file=sys.stderr,
        )
        print(USAGE, file=sys.stderr)
        return 2
    return Path(args[0]) if args else Path(__file__).parent


def main() -> int:
    root_or_code = parse_root(sys.argv)
    if isinstance(root_or_code, int):
        return root_or_code
    root = root_or_code
    # Guarded reads (#1141): a missing or unreadable package file is
    # an invocation error (exit 2, one line naming the file), not a
    # traceback at exit 1 where callers read "lockfiles disagree".
    for name in ("package.json", "package-lock.json", "bun.lock"):
        if not (root / name).is_file():
            return invocation_error(f"{root / name}: no such file")
    try:
        package = json.loads((root / "package.json").read_text())
        # Shape validation (#1159), as in npm_versions: wrong-shaped
        # package.json content is an invocation error, not a traceback.
        if not isinstance(package, dict):
            return invocation_error(
                f"{root / 'package.json'}: expected a JSON object"
            )
        for field in ("dependencies", "devDependencies"):
            if not isinstance(package.get(field, {}), dict):
                return invocation_error(
                    f"{root / 'package.json'}: '{field}' must be an object"
                )
        npm = npm_versions(root / "package-lock.json")
        bun = bun_versions(root / "bun.lock")
    except (OSError, json.JSONDecodeError, ValueError) as exc:
        return invocation_error(str(exc))
    deps = {
        **package.get("dependencies", {}),
        **package.get("devDependencies", {}),
    }
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
