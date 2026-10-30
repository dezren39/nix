#!/usr/bin/env python3
"""Invocation probes for check-lock-sync.py (#1141).

Before #1141 the script validated nothing about its invocation:
unknown flags and missing directories crashed with a traceback at
exit 1 — the same code as "lockfiles out of sync", which the
bun-sync workflow branches on — and extra arguments were silently
ignored. These probes pin the new contract:

- --help prints usage, exit 0.
- An unknown flag or an extra positional argument exits 2 with a
  usage line naming the offending argument, before any file is read.
- A missing package dir or a missing package.json / package-lock.json
  / bun.lock exits 2 with a one-line error naming the file — no
  traceback.
- The old signals are unchanged: 0 = in sync, 1 = out of sync with
  the original message.

Run: python3 check-lock-sync.test.py  (exit 0 = all probes passed)
"""

import json
import os
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPT = os.path.join(HERE, "check-lock-sync.py")
FAILURES = []


def run(*args):
    return subprocess.run(
        [sys.executable, SCRIPT, *args], capture_output=True, text=True
    )


def check(name, ok, detail=""):
    if ok:
        print(f"ok: {name}")
    else:
        FAILURES.append(name)
        print(f"FAIL: {name} {detail}")


def expect_invocation_error(name, result, needle):
    check(
        name,
        result.returncode == 2
        and needle in result.stderr
        and "Traceback" not in result.stderr,
        f"(rc={result.returncode}, stderr={result.stderr!r})",
    )


def write(path, text):
    with open(path, "w") as fh:
        fh.write(text)


def seed_package(dirpath, bun_version="1.0.0", with_bun=True):
    os.makedirs(dirpath, exist_ok=True)
    write(
        os.path.join(dirpath, "package.json"),
        json.dumps({"dependencies": {"example-dep": "^1.0.0"}}),
    )
    write(
        os.path.join(dirpath, "package-lock.json"),
        json.dumps(
            {"packages": {"node_modules/example-dep": {"version": "1.0.0"}}}
        ),
    )
    if with_bun:
        write(
            os.path.join(dirpath, "bun.lock"),
            '"example-dep": ["example-dep@%s", "", {}, "sha512-x"]' % bun_version,
        )


def main():
    # --help prints usage and exits 0.
    result = run("--help")
    check(
        "--help prints usage, exit 0",
        result.returncode == 0 and "usage:" in result.stdout,
        f"(rc={result.returncode}, stdout={result.stdout!r})",
    )

    # Unknown flag: exit 2 naming the flag, no traceback.
    expect_invocation_error("unknown flag", run("--bogus"), "--bogus")

    # Extra positional argument: exit 2 naming it. The first dir is a
    # real in-sync tree, so exit 0 here would prove the second
    # argument was silently dropped (the pre-#1141 behaviour).
    expect_invocation_error(
        "extra argument",
        run(HERE, "/nonexistent-xyz"),
        "/nonexistent-xyz",
    )

    # Empty-string dir must not silently become the caller's cwd.
    expect_invocation_error("empty argument", run(""), "empty")

    with tempfile.TemporaryDirectory() as tmp:
        # Missing dir: exit 2 naming the missing package.json.
        missing = os.path.join(tmp, "nope")
        expect_invocation_error(
            "missing dir", run(missing), os.path.join(missing, "package.json")
        )

        # Missing lockfile: exit 2 naming bun.lock.
        partial = os.path.join(tmp, "partial")
        seed_package(partial, with_bun=False)
        expect_invocation_error(
            "missing bun.lock", run(partial), os.path.join(partial, "bun.lock")
        )

        # In-sync fixture: exit 0.
        synced = os.path.join(tmp, "synced")
        seed_package(synced)
        result = run(synced)
        check(
            "in-sync fixture exits 0",
            result.returncode == 0 and "lockfiles in sync" in result.stdout,
            f"(rc={result.returncode}, stdout={result.stdout!r})",
        )

        # Desynced fixture: exit 1 with the original out-of-sync message.
        desynced = os.path.join(tmp, "desynced")
        seed_package(desynced, bun_version="2.0.0")
        result = run(desynced)
        check(
            "desynced fixture exits 1, message unchanged",
            result.returncode == 1
            and "lockfiles out of sync" in result.stderr,
            f"(rc={result.returncode}, stderr={result.stderr!r})",
        )

    # Baselines on the real tree: no-arg and single-dir calls exit 0.
    for name, args in (("no-arg baseline", ()), ("dir baseline", (HERE,))):
        result = run(*args)
        check(
            name,
            result.returncode == 0 and "lockfiles in sync" in result.stdout,
            f"(rc={result.returncode}, stdout={result.stdout!r}, "
            f"stderr={result.stderr!r})",
        )

    if FAILURES:
        print(f"{len(FAILURES)} probe(s) failed")
        return 1
    print("all probes passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
