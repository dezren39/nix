#!/usr/bin/env python3
"""Compare one conformance run with the records kept in a GitHub variable.

Usage:
  conformance-record.py <leg> <passed> <failed> <run-id/attempt> <now-iso>
                        <stored-json-or-empty> <summary-file> <new-value-file>
                        [<total-checks> <warnings> <skipped> <info> <skipped-names-json>]

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

Two records per leg, both kept in one variable as JSON, each always one whole
run -- a record is replaced, never patched in place:

  best     the lowest share of checks not passed (failed + skipped), at a total no
           smaller than the record's.
           Lower share, same or larger total          -> replaced.
           Same share, same or larger total, more passed -> replaced.
           Same share, same or larger total, as many  -> latest_timestamp moves to now.
           Anything else                              -> a large warning on the summary.

  largest  the most checks the suite recorded (every status).
           More          -> replaced.
           The same      -> counts updated to this run, first_timestamp kept.
           Fewer         -> a large warning on the summary.

A record that predates total_count or skipped_count is replaced by the next
run that reports them, rather than compared against.

Writes the new variable value to <new-value-file> only when it differs from the
stored one; the workflow decides whether to save it (main only). Exits 3 when
more checks were skipped than the best record, so the job can fail after every
leg is saved; otherwise 0.
"""
import json
import sys


def record(passed, failed, run, now, checks=None, warnings=0, skipped=None, info=0, names=None):
    r = {"first_timestamp": now, "latest_timestamp": now, "run": run,
         "passed_count": passed, "failed_count": failed}
    if checks is not None:
        r["total_count"] = checks
        r["warning_count"] = warnings
    if skipped is not None:
        r["skipped_count"] = skipped
        r["info_count"] = info
    if names is not None:
        r["skipped"] = sorted(names)
    return r


def total(r):
    # Records written before total_count existed counted passed+failed.
    return r.get("total_count", r["passed_count"] + r["failed_count"])


def describe(r):
    t = total(r)
    not_passed = r["failed_count"] + r.get("skipped_count", 0)
    pct = (100.0 * not_passed / t) if t else 0.0
    of = f"of {t} checks" if "total_count" in r else "total not recorded"
    return (f"{r['passed_count']} passed, {r['failed_count']} failed, "
            f"{r.get('skipped_count', '?')} skipped, {r.get('warning_count', 0)} warnings, "
            f"{r.get('info_count', 0)} info, {of} "
            f"({pct:.1f}% not passed) -- run {r['run']}, first {r['first_timestamp']}, "
            f"latest {r['latest_timestamp']}")


def bad(r):
    """Checks that did not pass: failed plus skipped. A skipped check passes
    nothing, so a run that skips fewer is better even at zero failures."""
    return r["failed_count"] + r.get("skipped_count", 0)


def complete(r):
    """Whether a record carries every count the comparison needs. Records
    written before total_count and skipped_count existed do not, and one was
    later patched in place into a mix of two runs; such a record is replaced
    rather than compared against."""
    return r is not None and "total_count" in r and "skipped_count" in r


def compare(leg, passed, failed, run, now, stored, checks=None, warnings_count=0,
            skipped=None, info=0, names=None):
    """Returns (new_state, lines, warnings, errors).

    Both records are always whole runs: a record is replaced, never patched.

    best     the lowest share of checks not passed (failed + skipped) at a total
             no smaller than the record's. Lower replaces it; equal with more
             passed replaces it; equal with as many passed moves
             latest_timestamp; anything else warns.
    largest  the most checks the suite recorded. More replaces it; the same
             number replaces its counts and keeps first_timestamp; fewer warns.

    A run with more skipped checks than the best record is an error.
    """
    cur = record(passed, failed, run, now, checks, warnings_count, skipped, info, names)
    state = json.loads(json.dumps(stored)) if stored else {}
    lines, warnings, errors = [], [], []
    cur_complete = complete(cur)

    for k in ("best", "largest"):
        if k in state and cur_complete and not complete(state[k]):
            lines.append(f"{k}: the record predates full counts ({describe(state[k])}); "
                         "this run replaces it")
            state[k] = cur

    best = state.get("best")
    if (cur_complete and complete(best) and best is not cur
            and cur["skipped_count"] > best["skipped_count"]):
        newly = sorted(set(names or []) - set(best.get("skipped", [])))
        errors.append(f"skipped checks rose from {best['skipped_count']} to {cur['skipped_count']}.\n"
                      f"  this run: {describe(cur)}\n  record:   {describe(best)}"
                      + (f"\n  newly skipped: {', '.join(newly)}" if newly else ""))

    if best is None:
        state["best"] = cur
        lines.append("best: no record yet; this run becomes it")
    elif best is not cur:
        t, bt = total(cur), total(best)
        # bad/total compared without division: a/b < c/d  <=>  a*d < c*b.
        lhs, rhs = bad(cur) * bt, bad(best) * t
        if t == 0:
            warnings.append(f"best: this run produced no checks at all; record is {describe(best)}")
        elif t >= bt and not errors and (lhs < rhs or (lhs == rhs and passed > best["passed_count"])):
            state["best"] = cur
            lines.append(f"best: **improved**, replacing {describe(best)}")
        elif t >= bt and lhs == rhs and passed == best["passed_count"]:
            state["best"] = dict(best, latest_timestamp=now)
            lines.append("best: matched the record; latest_timestamp updated")
        else:
            why = ("fewer checks ran than in the record" if t < bt
                   else "a larger share of checks did not pass than in the record")
            warnings.append(f"best: {why}.\n  this run: {describe(cur)}\n  record:   {describe(best)}")

    largest = state.get("largest")
    if largest is None:
        state["largest"] = cur
        lines.append("largest: no record yet; this run becomes it")
    elif largest is not cur:
        t, lt = total(cur), total(largest)
        if t > lt:
            state["largest"] = cur
            lines.append(f"largest: **more checks ran**, replacing {describe(largest)}")
        elif t == lt:
            state["largest"] = dict(cur, first_timestamp=largest["first_timestamp"])
            lines.append("largest: same number of checks; counts updated to this run")
        else:
            warnings.append(f"largest: fewer checks ran than in the record "
                            f"({t} against {lt}).\n  this run: {describe(cur)}\n  record:   {describe(largest)}")
    return state, lines, warnings, errors


def main(argv):
    leg, passed, failed, run, now, stored_raw, summary, out = argv[1:9]
    passed, failed = int(passed), int(failed)
    checks = int(argv[9]) if len(argv) > 9 and argv[9] else None
    warn_n = int(argv[10]) if len(argv) > 10 and argv[10] else 0
    skipped = int(argv[11]) if len(argv) > 11 and argv[11] else None
    info = int(argv[12]) if len(argv) > 12 and argv[12] else 0
    names = json.loads(argv[13]) if len(argv) > 13 and argv[13].strip() else None
    stored = json.loads(stored_raw) if stored_raw.strip() else None
    state, lines, warnings, errors = compare(leg, passed, failed, run, now, stored, checks,
                                             warn_n, skipped, info, names)

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
