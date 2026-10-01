#!/usr/bin/env python3
"""Summarise `go test -json` output and hold the skipped count to a ceiling.

Usage: gotest-report.py <go-test-json-file> <max-skipped> <summary-file>

Writes a table of passed / failed / skipped tests to <summary-file>, listing
every skipped test with the reason it gave. Exits 1 when any test failed or
more tests were skipped than <max-skipped>. A skipped test passes nothing, so
a rise is a regression even when everything that ran passed. When fewer are
skipped than the ceiling it warns, so the ceiling is lowered on purpose
rather than left with room for a skip to creep back in.
"""
import collections
import json
import sys


def main(path, ceiling, summary):
    ceiling = int(ceiling)
    counts = collections.Counter()
    out = collections.defaultdict(list)
    skipped = []
    failed = []
    for line in open(path, errors="replace"):
        try:
            e = json.loads(line)
        except ValueError:
            continue
        test = e.get("Test")
        if not test:
            continue
        key = (e.get("Package", ""), test)
        act = e.get("Action")
        if act == "output":
            out[key].append(e.get("Output", ""))
        elif act in ("pass", "fail", "skip"):
            counts[act] += 1
            if act == "skip":
                skipped.append(key)
            elif act == "fail":
                failed.append(key)

    def reason(key):
        lines = [o.strip() for o in out[key]
                 if o.strip() and not o.lstrip().startswith(("=== ", "--- "))]
        return (lines[-1] if lines else "(no reason given)")[:200]

    n_skip = len(skipped)
    over = n_skip > ceiling
    with open(summary, "a") as f:
        f.write("## Go tests\n\n| passed | failed | skipped | skip ceiling |\n| --- | --- | --- | --- |\n")
        f.write(f"| {counts['pass']} | {counts['fail']} | **{n_skip}** | {ceiling} |\n\n")
        if failed:
            f.write("> [!CAUTION]\n> # Failed tests\n")
            for p, t in failed:
                f.write(f"> - `{p.rsplit('/', 1)[-1]}` {t}\n")
            f.write("\n")
        if over:
            f.write(f"> [!CAUTION]\n> # More tests skipped than the ceiling: {n_skip} against {ceiling}\n"
                    "> A skipped test passes nothing. Fix the new skip, or raise `GO_TEST_MAX_SKIPPED` in "
                    "`.github/workflows/mcpx.yml` on purpose and say why.\n\n")
        elif n_skip < ceiling:
            f.write(f"> [!WARNING]\n> Fewer tests skipped than the ceiling: {n_skip} against {ceiling}. "
                    f"Lower `GO_TEST_MAX_SKIPPED` to {n_skip} so a skip cannot creep back in unnoticed.\n\n")
        if skipped:
            f.write("<details><summary>Every skipped test, with its reason</summary>\n\n"
                    "| package | test | reason |\n| --- | --- | --- |\n")
            for key in sorted(skipped):
                r = reason(key).replace("|", "\\|")
                f.write(f"| `{key[0].rsplit('/', 1)[-1]}` | {key[1]} | {r} |\n")
            f.write("\n</details>\n")
    print(f"go tests: {counts['pass']} passed, {counts['fail']} failed, {n_skip} skipped (ceiling {ceiling})")
    if over:
        print(f"::error::{n_skip} Go tests skipped, more than the ceiling of {ceiling}")
    elif n_skip < ceiling:
        print(f"::warning::{n_skip} Go tests skipped, below the ceiling of {ceiling}; lower GO_TEST_MAX_SKIPPED")
    return 1 if (failed or over) else 0


if __name__ == "__main__":
    sys.exit(main(*sys.argv[1:4]))
