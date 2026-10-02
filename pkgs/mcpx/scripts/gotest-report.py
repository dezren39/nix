#!/usr/bin/env python3
"""Summarise `go test -json` output and hold skipped tests to the record on main.

Usage:
  gotest-report.py <go-test-json-file> <stored-record-json-or-empty> <run-id/attempt>
                   <now-iso> <summary-file> <new-record-file>

The record lives in a repository variable, like the conformance records, and
holds the counts and the name of every skipped test. A run that skips more
tests than the record fails; the newly skipped tests are named. The record
only moves down: a run with fewer skips becomes the new ceiling, so a skip
cannot creep back in. With no record yet the run is reported and becomes the
record when saved on main.

Exits 1 when any test failed or more tests were skipped than the record.
<new-record-file> is written when the record should change; saving it is the
workflow's decision, on main only.
"""
import collections
import json
import sys


def read(path):
    counts = collections.Counter()
    out = collections.defaultdict(list)
    skipped, failed = [], []
    for line in open(path, errors="replace"):
        try:
            e = json.loads(line)
        except ValueError:
            continue
        test = e.get("Test")
        if not test:
            continue
        key = f"{e.get('Package', '').rsplit('/', 1)[-1]}.{test}"
        act = e.get("Action")
        if act == "output":
            out[key].append(e.get("Output", ""))
        elif act in ("pass", "fail", "skip"):
            counts[act] += 1
            (skipped if act == "skip" else failed if act == "fail" else []).append(key)
    reasons = {}
    for k in skipped:
        lines = [o.strip() for o in out[k] if o.strip() and not o.lstrip().startswith(("=== ", "--- "))]
        reasons[k] = (lines[-1] if lines else "(no reason given)")[:200]
    return counts, sorted(skipped), sorted(failed), reasons


def decide(skipped, passed, failed_n, stored, run, now):
    """Returns (new_record_or_None, rose, new_names, gone_names)."""
    cur = {"timestamp": now, "run": run, "passed_count": passed, "failed_count": failed_n,
           "skipped_count": len(skipped), "skipped": skipped}
    if not stored:
        return cur, False, [], []
    prev = set(stored.get("skipped", []))
    new_names = sorted(set(skipped) - prev)
    gone_names = sorted(prev - set(skipped))
    if len(skipped) > stored["skipped_count"]:
        return None, True, new_names, gone_names
    if len(skipped) < stored["skipped_count"] or skipped != stored.get("skipped"):
        return cur, False, new_names, gone_names
    return None, False, new_names, gone_names


def main(path, stored_raw, run, now, summary, new_file):
    counts, skipped, failed, reasons = read(path)
    stored = json.loads(stored_raw) if stored_raw.strip() else None
    new, rose, new_names, gone = decide(skipped, counts["pass"], counts["fail"], stored, run, now)
    ceiling = stored["skipped_count"] if stored else None

    with open(summary, "a") as f:
        f.write("## Go tests\n\n| passed | failed | skipped | record on main |\n| --- | --- | --- | --- |\n")
        f.write(f"| {counts['pass']} | {counts['fail']} | **{len(skipped)}** | "
                f"{ceiling if ceiling is not None else 'none yet'}"
                f"{' (run ' + stored['run'] + ')' if stored else ''} |\n\n")
        if failed:
            f.write("> [!CAUTION]\n> # Failed tests\n" + "".join(f"> - {k}\n" for k in failed) + "\n")
        if rose:
            f.write(f"> [!CAUTION]\n> # More tests skipped than the record: {len(skipped)} against {ceiling}\n"
                    "> A skipped test passes nothing. Newly skipped:\n")
            for k in new_names:
                f.write(f"> - `{k}` -- {reasons.get(k, '')}\n")
            f.write("\n")
        elif stored and len(skipped) < ceiling:
            f.write(f"Skipped tests **down** from {ceiling} to {len(skipped)}"
                    f"{' (no longer skipped: ' + ', '.join(f'`{k}`' for k in gone) + ')' if gone else ''}. "
                    "On main this becomes the new record.\n\n")
        elif stored and (new_names or gone):
            f.write(f"Same number skipped, different tests. Newly skipped: "
                    f"{', '.join(f'`{k}`' for k in new_names) or 'none'}; no longer skipped: "
                    f"{', '.join(f'`{k}`' for k in gone) or 'none'}.\n\n")
        if skipped:
            f.write("<details><summary>Every skipped test, with its reason</summary>\n\n"
                    "| test | reason |\n| --- | --- |\n")
            for k in skipped:
                f.write(f"| `{k}` | {reasons[k].replace('|', chr(92) + '|')} |\n")
            f.write("\n</details>\n")

    print(f"go tests: {counts['pass']} passed, {counts['fail']} failed, {len(skipped)} skipped "
          f"(record: {ceiling if ceiling is not None else 'none yet'})")
    for k in skipped:
        print(f"  skip  {k}  -- {reasons[k]}")
    if rose:
        print(f"::error::{len(skipped)} Go tests skipped, more than the record of {ceiling}; newly skipped: "
              + ", ".join(new_names))
    if new is not None:
        with open(new_file, "w") as f:
            json.dump(new, f, separators=(",", ":"))
    return 1 if (failed or rose) else 0


if __name__ == "__main__":
    sys.exit(main(*sys.argv[1:7]))
