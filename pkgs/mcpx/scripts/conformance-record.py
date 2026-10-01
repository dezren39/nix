#!/usr/bin/env python3
"""Compare one conformance run with the records kept in a GitHub variable.

Usage:
  conformance-record.py <leg> <passed> <failed> <run-id/attempt> <now-iso>
                        <stored-json-or-empty> <summary-file> <new-value-file>
                        [<total-checks> <warnings> <skipped> <info>]

<total-checks> is every check the suite recorded in checks.json, whatever its
status -- SUCCESS, FAILURE, WARNING, INFO, SKIPPED. The summary line's
"N passed, M failed" leaves warnings out, so a failure that becomes a
warning shrank passed+failed, and a run with fewer failures read as one where
fewer checks ran. When it is not given, passed+failed is used.

<skipped> is the number of checks the suite recorded as SKIPPED. A run with
more skipped checks than the best record fails this step, whatever else it
does: a skipped check passes nothing, and an increase usually means a fixture
or a capability went missing. Records written before skipped_count existed
are not compared.

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


def record(passed, failed, run, now, checks=None, warnings=0, skipped=None, info=0):
    r = {"first_timestamp": now, "latest_timestamp": now, "run": run,
         "passed_count": passed, "failed_count": failed}
    if checks is not None:
        r["total_count"] = checks
        r["warning_count"] = warnings
    if skipped is not None:
        r["skipped_count"] = skipped
        r["info_count"] = info
    return r


def total(r):
    # Records written before total_count existed counted passed+failed.
    return r.get("total_count", r["passed_count"] + r["failed_count"])


def describe(r):
    t = total(r)
    pct = (100.0 * r["failed_count"] / t) if t else 0.0
    return (f"{r['passed_count']} passed, {r['failed_count']} failed, "
            f"{r.get('skipped_count', '?')} skipped, {r.get('warning_count', 0)} warnings, "
            f"{r.get('info_count', 0)} info, of {t} checks "
            f"({pct:.1f}% failed) -- run {r['run']}, first {r['first_timestamp']}, "
            f"latest {r['latest_timestamp']}")


def compare(leg, passed, failed, run, now, stored, checks=None, warnings_count=0,
            skipped=None, info=0):
    """Returns (new_state, lines, warnings, errors)."""
    cur = record(passed, failed, run, now, checks, warnings_count, skipped, info)
    state = json.loads(json.dumps(stored)) if stored else {}
    lines, warnings, errors = [], [], []

    best0 = state.get("best")
    if skipped is not None and best0 and "skipped_count" in best0 and skipped > best0["skipped_count"]:
        errors.append(f"skipped checks rose from {best0['skipped_count']} to {skipped}.\n"
                      f"  this run: {describe(cur)}\n  record:   {describe(best0)}")

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
        elif t >= bt and lhs < rhs and not errors:
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
    # The skip baseline only moves down: fewer skips becomes the new ceiling,
    # so a later run cannot climb back to the old number unnoticed.
    if (skipped is not None and not errors and "best" in state
            and "skipped_count" in state["best"] and skipped < state["best"]["skipped_count"]):
        lines.append(f"skipped checks: **down** from {state['best']['skipped_count']} to {skipped}; "
                     "that is the new ceiling")
        state["best"] = dict(state["best"], skipped_count=skipped)
    # A record that predates skipped_count learns it from the first run that
    # reports one, so the comparison starts from there.
    if skipped is not None and not errors:
        for k in ("best", "largest"):
            if k in state and "skipped_count" not in state[k]:
                state[k] = dict(state[k], skipped_count=skipped, info_count=info)
    return state, lines, warnings, errors


def main(argv):
    leg, passed, failed, run, now, stored_raw, summary, out = argv[1:9]
    passed, failed = int(passed), int(failed)
    checks = int(argv[9]) if len(argv) > 9 and argv[9] else None
    warn_n = int(argv[10]) if len(argv) > 10 and argv[10] else 0
    skipped = int(argv[11]) if len(argv) > 11 and argv[11] else None
    info = int(argv[12]) if len(argv) > 12 and argv[12] else 0
    stored = json.loads(stored_raw) if stored_raw.strip() else None
    state, lines, warnings, errors = compare(leg, passed, failed, run, now, stored, checks,
                                             warn_n, skipped, info)

    with open(summary, "a") as f:
        f.write(f"\n### Records for `{leg}`\n\n")
        for line in lines:
            f.write(f"- {line}\n")
        for e in errors:
            f.write("\n> [!CAUTION]\n> # More checks skipped: `" + leg + "` -- this fails the job\n")
            for el in e.splitlines():
                f.write(f"> {el}\n")
        for w in warnings:
            f.write("\n> [!CAUTION]\n> # Conformance went backwards: `" + leg + "`\n")
            for wl in w.splitlines():
                f.write(f"> {wl}\n")
        if not lines and not warnings and not errors:
            f.write("- nothing to compare\n")

    if state != (stored or {}):
        with open(out, "w") as f:
            json.dump(state, f, separators=(",", ":"))
    for w in warnings:
        print(f"::error title=conformance regressed ({leg})::{w.splitlines()[0]}")
    for e in errors:
        print(f"::error title=more checks skipped ({leg})::{e.splitlines()[0]}")
    # Exit 3 on a skip increase, so the workflow can fail the job after every
    # leg has been compared and saved.
    return 3 if errors else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
