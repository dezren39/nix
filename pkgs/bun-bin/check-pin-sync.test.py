#!/usr/bin/env python3
"""Probes for check-pin-sync.py (#1148).

Before #1148 the script read both pin files unguarded: a missing
workflow file, a missing hashes.json, or malformed JSON in
hashes.json crashed with a traceback at exit 1 — the same code the
script documents for "pins out of sync", which the bun-pin-sync CI
job branches on. These probes pin the new contract:

- A missing workflow file or missing hashes.json exits 2 with a
  one-line error naming the file — no traceback.
- Malformed hashes.json exits 2 naming the file and the JSON
  problem — no traceback.
- A non-UTF-8 workflow file or hashes.json exits 2 naming the
  file — no traceback (#1189: the workflow read's OSError guard
  did not cover UnicodeDecodeError, so it trace-backed at exit 1).
- The old signals are unchanged: 0 = in sync, 1 = out of sync with
  both pins named, and a repeated --repo-root exits 2 with usage.

Run: python3 check-pin-sync.test.py  (exit 0 = all probes passed)
"""

import json
import os
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPT = os.path.join(HERE, "check-pin-sync.py")
WORKFLOW = os.path.join(".github", "workflows", "buffer-backup-renovate-bun-sync.yml")
HASHES = os.path.join("pkgs", "bun-bin", "hashes.json")
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
        and "Traceback" not in result.stderr
        and result.stderr.count("\n") <= 1,
        f"(rc={result.returncode}, stderr={result.stderr!r})",
    )


def write(path, text):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as fh:
        fh.write(text)


def write_bytes(path, data):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "wb") as fh:
        fh.write(data)


def seed_root(dirpath, workflow_version="1.3.14", hashes_version="1.3.14",
              with_workflow=True, with_hashes=True, hashes_text=None):
    if with_workflow:
        write(
            os.path.join(dirpath, WORKFLOW),
            f'        bun-version: "{workflow_version}"\n',
        )
    if with_hashes:
        write(
            os.path.join(dirpath, HASHES),
            hashes_text
            if hashes_text is not None
            else json.dumps({"version": hashes_version, "hashes": {}}),
        )


def main():
    with tempfile.TemporaryDirectory() as tmp:
        # Missing workflow file: exit 2 naming the workflow, no traceback.
        empty = os.path.join(tmp, "empty")
        os.makedirs(empty)
        expect_invocation_error(
            "missing workflow", run("--repo-root", empty),
            os.path.join(empty, WORKFLOW),
        )

        # Missing repo root entirely: same guarded path.
        missing = os.path.join(tmp, "nope")
        expect_invocation_error(
            "missing repo root", run("--repo-root", missing),
            os.path.join(missing, WORKFLOW),
        )

        # Missing hashes.json: exit 2 naming hashes.json.
        no_hashes = os.path.join(tmp, "no-hashes")
        seed_root(no_hashes, with_hashes=False)
        expect_invocation_error(
            "missing hashes", run("--repo-root", no_hashes),
            os.path.join(no_hashes, HASHES),
        )

        # Malformed hashes.json: exit 2 naming the file and the problem.
        malformed = os.path.join(tmp, "malformed")
        seed_root(malformed, hashes_text="{not json")
        result = run("--repo-root", malformed)
        expect_invocation_error(
            "malformed hashes", result, os.path.join(malformed, HASHES)
        )
        check(
            "malformed hashes names the JSON problem",
            "invalid JSON" in result.stderr,
            f"(stderr={result.stderr!r})",
        )

        # Non-UTF-8 workflow file: exit 2 naming the workflow, no
        # traceback (#1189 — UnicodeDecodeError escaped the OSError
        # guard and trace-backed at exit 1, the drift signal).
        bad_workflow = os.path.join(tmp, "bad-workflow")
        seed_root(bad_workflow)
        write_bytes(
            os.path.join(bad_workflow, WORKFLOW),
            b'        bun-version: "1.3.14" \xff\xfe\n',
        )
        expect_invocation_error(
            "non-UTF-8 workflow", run("--repo-root", bad_workflow),
            os.path.join(bad_workflow, WORKFLOW),
        )

        # Non-UTF-8 hashes.json (control): already exit 2 via the
        # ValueError branch — pin it so the two guards stay symmetric.
        bad_hashes = os.path.join(tmp, "bad-hashes")
        seed_root(bad_hashes)
        write_bytes(
            os.path.join(bad_hashes, HASHES),
            b'{"version": "1.3.14" \xff\xfe}',
        )
        expect_invocation_error(
            "non-UTF-8 hashes", run("--repo-root", bad_hashes),
            os.path.join(bad_hashes, HASHES),
        )

        # In-sync fixture: exit 0 with the original message.
        synced = os.path.join(tmp, "synced")
        seed_root(synced)
        result = run("--repo-root", synced)
        check(
            "in-sync fixture exits 0",
            result.returncode == 0 and "bun pins in sync" in result.stdout
            and "1.3.14" in result.stdout,
            f"(rc={result.returncode}, stdout={result.stdout!r})",
        )

        # Desynced fixture: exit 1 naming both pins and versions.
        desynced = os.path.join(tmp, "desynced")
        seed_root(desynced, hashes_version="9.9.9")
        result = run("--repo-root", desynced)
        check(
            "desynced fixture exits 1, both pins named",
            result.returncode == 1
            and "bun pins out of sync" in result.stderr
            and "1.3.14" in result.stderr
            and "9.9.9" in result.stderr,
            f"(rc={result.returncode}, stderr={result.stderr!r})",
        )

    # Repeated --repo-root: the pre-existing usage path, exit 2.
    result = run("--repo-root", HERE, "--repo-root", HERE)
    check(
        "repeated --repo-root exits 2 with usage",
        result.returncode == 2 and "usage:" in result.stderr
        and "Traceback" not in result.stderr,
        f"(rc={result.returncode}, stderr={result.stderr!r})",
    )

    # Baseline on the real tree: the no-arg call exits 0. This relies
    # on the script's repo-root layout (pkgs/bun-bin two levels below
    # the root holding both pin files), which the flake check
    # reproduces when it copies the suite into its check tree.
    result = run()
    check(
        "no-arg baseline exits 0",
        result.returncode == 0 and "bun pins in sync" in result.stdout,
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
