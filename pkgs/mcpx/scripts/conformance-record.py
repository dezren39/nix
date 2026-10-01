#!/usr/bin/env python3
"""Compare one conformance run with the records kept in a GitHub variable.

Usage:
  conformance-record.py <leg> <passed> <failed> <run-id/attempt> <now-iso>
                        <stored-json-or-empty> <summary-file> <new-value-file>
                        [<total-checks> <warnings>]

<total-checks> is every check the suite recorded in checks.json, whatever its
status -- SUCCESS, FAILURE, WARNING, INFO, SKIPPED. The summary line's
"N passed, M failed" leaves warnings out, so a failure that becomes a
warning shrank passed+failed, and a run with fewer failures read as one where
fewer checks ran. When it is not given, passed+failed is used.

Two records per leg, both kept in one variable as JSON:

  best     the lowest failed/total ratio, at a total no smaller than the record's.
           Lower ratio with the same or larger total  -> replaced (first = latest = now).
           Same ratio with the same or larger total   -> latest_timestamp moves to now.
           Anything else                              -> a large warning on the summary.

  largest  the largest passed+failed.
           Larger sum   -> replaced.
           Same sum     -> latest_timestamp moves to now.
           Smaller sum  -> a large warning on the summary.

Writes the new variable value to <new-value-file> only when it differs from the stored one;
the workflow decides whether to save it (main only). Exits 0: a conformance regression is
reported, never a reason for this step to fail.
"""
import json
import sys


def record(passed, failed, run, now, checks=None, warnings=0):
    r = {"first_timestamp": now, "latest_timestamp": now, "run": run,
         "passed_count": passed, "failed_count": failed}
    if checks is not None:
        r["total_count"] = checks
        r["warning_count"] = warnings
    return r


def total(r):
    # Records written before total_count existed counted passed+failed.
    return r.get("total_count", r["passed_count"] + r["failed_count"])


def describe(r):
    t = total(r)
    pct = (100.0 * r["failed_count"] / t) if t else 0.0
    return (f"{r['passed_count']} passed, {r['failed_count']} failed, "
            f"{r.get('warning_count', 0)} warnings, of {t} checks "
            f"({pct:.1f}% failed) -- run {r['run']}, first {r['first_timestamp']}, "
            f"latest {r['latest_timestamp']}")


def compare(leg, passed, failed, run, now, stored, checks=None, warnings_count=0):
    """Returns (new_state, lines, warnings)."""
    cur = record(passed, failed, run, now, checks, warnings_count)
    state = json.loads(json.dumps(stored)) if stored else {}
    lines, warnings = [], []

    best = state.get("best")
    if best is None:
        state["best"] = cur
        lines.append("best ratio: no record yet; this run becomes it")
    else:
        t, bt = total(cur), total(best)
        # failed/total compared without division: a/b < c/d  <=>  a*d < c*b (b, d > 0).
        lhs, rhs = cur["failed_count"] * bt, best["failed_count"] * t
        if t == 0:
            warnings.append(f"best ratio: this run produced no checks at all; record is {describe(best)}")
        elif t >= bt and lhs < rhs:
            state["best"] = cur
            lines.append(f"best ratio: **improved**, replacing {describe(best)}")
        elif t >= bt and lhs == rhs:
            state["best"] = dict(best, latest_timestamp=now)
            lines.append("best ratio: matched the record; latest_timestamp updated")
        else:
            why = ("fewer checks ran than in the record" if t < bt
                   else "a higher share of checks failed than in the record")
            warnings.append(f"best ratio: {why}.\n  this run: {describe(cur)}\n  record:   {describe(best)}")

    largest = state.get("largest")
    if largest is None:
        state["largest"] = cur
        lines.append("largest total: no record yet; this run becomes it")
    else:
        t, lt = total(cur), total(largest)
        if t > lt:
            state["largest"] = cur
            lines.append(f"largest total: **more checks ran**, replacing {describe(largest)}")
        elif t == lt:
            state["largest"] = dict(largest, latest_timestamp=now)
            lines.append("largest total: matched the record; latest_timestamp updated")
        else:
            warnings.append(f"largest total: fewer checks ran than in the record "
                            f"({t} against {lt}).\n  this run: {describe(cur)}\n  record:   {describe(largest)}")
    return state, lines, warnings


def main(argv):
    leg, passed, failed, run, now, stored_raw, summary, out = argv[1:9]
    passed, failed = int(passed), int(failed)
    checks = int(argv[9]) if len(argv) > 9 and argv[9] else None
    warn_n = int(argv[10]) if len(argv) > 10 and argv[10] else 0
    stored = json.loads(stored_raw) if stored_raw.strip() else None
    state, lines, warnings = compare(leg, passed, failed, run, now, stored, checks, warn_n)

    with open(summary, "a") as f:
        f.write(f"\n### Records for `{leg}`\n\n")
        for line in lines:
            f.write(f"- {line}\n")
        for w in warnings:
            f.write("\n> [!CAUTION]\n> # Conformance went backwards: `" + leg + "`\n")
            for wl in w.splitlines():
                f.write(f"> {wl}\n")
        if not lines and not warnings:
            f.write("- nothing to compare\n")

    if state != (stored or {}):
        with open(out, "w") as f:
            json.dump(state, f, separators=(",", ":"))
    for w in warnings:
        print(f"::error title=conformance regressed ({leg})::{w.splitlines()[0]}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
