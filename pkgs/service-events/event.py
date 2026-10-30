"""Append one service lifecycle event to ~/Library/Logs/nix-services/events.jsonl.

As a command (`service-event SOURCE EVENT [key=value ...]`) it is how the
switch script, home-manager activation and the agents' start wrappers record
what they do; monitor.py imports the same functions. Values that look like
integers are stored as numbers, everything else as strings. Never pass a
secret: this log is meant to be read and pasted into issues. dezren39/nix#670.
"""

import datetime
import json
import os
import re
import shutil
import sys

LOG_DIR = os.path.expanduser("~/Library/Logs/nix-services")
LOG = os.path.join(LOG_DIR, "events.jsonl")
LOG_BYTES = 10 * 1024 * 1024
LOG_KEEP = 5

USAGE = "usage: service-event SOURCE EVENT [key=value ...]"
HELP_NOTE = (
    "--help/-h as the sole argument prints this usage and exits 0; "
    "--help/-h is never a SOURCE or EVENT name."
)


def rotate(path, max_bytes, keep, copy_truncate=False):
    """Shift path to path.1 .. path.<keep> once it passes max_bytes.

    copy_truncate is for files another process keeps open with O_APPEND
    (launchd's StandardErrorPath): the file is copied, then truncated in
    place, so the writer carries on at the new end. Returns the size rotated,
    or 0.
    """
    try:
        size = os.path.getsize(path)
    except OSError:
        return 0
    if size < max_bytes:
        return 0
    for i in range(keep - 1, 0, -1):
        if os.path.exists(f"{path}.{i}"):
            os.replace(f"{path}.{i}", f"{path}.{i + 1}")
    if copy_truncate:
        shutil.copyfile(path, f"{path}.1")
        with open(path, "r+") as fh:
            fh.truncate(0)
    else:
        os.replace(path, f"{path}.1")
    return size


def append_event(source, event, **fields):
    record = {
        "ts": datetime.datetime.now(datetime.timezone.utc).isoformat(timespec="milliseconds"),
        "source": source,
        "event": event,
    }
    record.update({k: v for k, v in fields.items() if v is not None})
    try:
        os.makedirs(LOG_DIR, exist_ok=True)
        rotate(LOG, LOG_BYTES, LOG_KEEP)
        with open(LOG, "a") as fh:
            fh.write(json.dumps(record, sort_keys=False) + "\n")
    except OSError as exc:
        # #934: report failure to the caller (the CLI turns it into a
        # non-zero exit) but never raise — monitor.py imports this and
        # must not die on a log-write failure.
        print(f"service-event: cannot write {LOG}: {exc}", file=sys.stderr)
        return False
    return True


def main(argv):
    args = argv[1:]
    # Contract rule 8 (#1205): --help/-h as the sole argument prints
    # usage on stdout and exits 0, appending nothing. In any other
    # position it is rejected before appending -- a --help in the
    # SOURCE position used to append a real event whose source was
    # the literal string "--help".
    if args in (["--help"], ["-h"]):
        print(USAGE)
        print(HELP_NOTE)
        return 0
    if "--help" in args or "-h" in args:
        bad = "--help" if "--help" in args else "-h"
        print(
            f"service-event: invalid argument {bad!r}:"
            " help is only valid as the sole argument",
            file=sys.stderr,
        )
        print(USAGE, file=sys.stderr)
        return 2
    if len(argv) < 3:
        print(USAGE, file=sys.stderr)
        return 2
    fields = {}
    for arg in argv[3:]:
        key, sep, value = arg.partition("=")
        # #913: a field that is not key=value (or has an empty key) used to
        # be silently dropped / stored under "", so the event was written
        # incomplete with exit 0. Fail loudly instead, before appending.
        if not sep or not key:
            print(
                f"service-event: invalid field {arg!r}: expected key=value",
                file=sys.stderr,
            )
            print(USAGE, file=sys.stderr)
            return 2
        # #919: ts/source/event are the record's own fields. A ts= field used
        # to silently overwrite the real timestamp via record.update(), and
        # source=/event= crashed append_event() with a TypeError traceback,
        # losing the event. Reject them the same loud way, before appending.
        if key in ("ts", "source", "event"):
            print(
                f"service-event: invalid field {arg!r}: {key!r} is a reserved field name",
                file=sys.stderr,
            )
            print(USAGE, file=sys.stderr)
            return 2
        # #1127: a duplicated field name used to silently last-win —
        # fields[key] below assigned unconditionally, so pid=1 pid=2
        # logged only pid=2 with exit 0 and the first value was recorded
        # nowhere. Reject the repeat the same loud way, before appending.
        if key in fields:
            print(
                f"service-event: duplicate field {arg!r}: {key!r} may be given only once",
                file=sys.stderr,
            )
            print(USAGE, file=sys.stderr)
            return 2
        # #935: gate on a plain ASCII integer shape, not str.isdigit() —
        # isdigit() is also true for Unicode digit characters int()
        # rejects (superscript/subscript digits), which used to crash
        # here with a traceback and lose the event. Digit-like Unicode
        # values are stored as strings, like any other non-integer.
        fields[key] = (
            int(value)
            if re.fullmatch(r"-?[0-9]+", value) and len(value) < 19
            else value
        )
    # #934: a failed write is not success — callers that guard with
    # || true are unaffected, everyone else now gets the real signal.
    return 0 if append_event(argv[1], argv[2], **fields) else 1


if __name__ == "__main__":
    sys.exit(main(sys.argv))
