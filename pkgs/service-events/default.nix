# service-monitor (the org.nixos.service-events agent) and service-event (one
# event from a script), both writing ~/Library/Logs/nix-services/events.jsonl.
# See monitor.py and docs/services.md; dezren39/nix#670.
{
  runCommand,
  python3,
  makeWrapper,
}:
runCommand "service-events"
  {
    nativeBuildInputs = [ makeWrapper ];
  }
  ''
    lib=$out/lib/service-events
    mkdir -p $lib $out/bin
    cp ${./event.py} $lib/event.py
    cp ${./monitor.py} $lib/monitor.py
    ${python3}/bin/python3 -m py_compile $lib/event.py $lib/monitor.py
    makeWrapper ${python3}/bin/python3 $out/bin/service-event --add-flags $lib/event.py
    makeWrapper ${python3}/bin/python3 $out/bin/service-monitor --add-flags $lib/monitor.py

    # Smoke test: an event is one valid JSON line with its fields typed, and
    # rotation shifts the file once it passes the limit.
    export HOME=$TMPDIR
    $out/bin/service-event test check.one pid=42 cause=deploy
    log=$HOME/Library/Logs/nix-services/events.jsonl
    # #913: malformed fields (no '=', or an empty key) exit 2 naming the
    # argument, and append nothing — the python check below still sees
    # exactly one line in the log.
    rc=0
    $out/bin/service-event test check.bad badarg pid=42 2>$TMPDIR/bad.err || rc=$?
    [ "$rc" -eq 2 ] || { echo "service-event no-'=' field: expected exit 2, got $rc" >&2; exit 1; }
    grep -q "invalid field 'badarg'" $TMPDIR/bad.err || { echo "service-event no-'=' field: argument not named in error" >&2; exit 1; }
    rc=0
    $out/bin/service-event test check.bad =novalue 2>$TMPDIR/empty.err || rc=$?
    [ "$rc" -eq 2 ] || { echo "service-event empty-key field: expected exit 2, got $rc" >&2; exit 1; }
    grep -q "invalid field '=novalue'" $TMPDIR/empty.err || { echo "service-event empty-key field: argument not named in error" >&2; exit 1; }
    # #918: a bad SERVICE_EVENTS_INTERVAL exits 2 at startup naming the
    # variable and value, instead of crashing at import/sleep or busy-looping.
    rc=0
    SERVICE_EVENTS_INTERVAL=abc $out/bin/service-monitor 2>$TMPDIR/interval.err || rc=$?
    [ "$rc" -eq 2 ] || { echo "service-monitor bad interval: expected exit 2, got $rc" >&2; exit 1; }
    grep -q "invalid SERVICE_EVENTS_INTERVAL='abc'" $TMPDIR/interval.err || { echo "service-monitor bad interval: variable/value not named in error" >&2; exit 1; }
    # #919: reserved field names (ts/source/event) exit 2 naming the field,
    # and append nothing — the python check below still sees exactly one line.
    rc=0
    $out/bin/service-event test check.bad ts=2000-01-01T00:00:00 2>$TMPDIR/reserved.err || rc=$?
    [ "$rc" -eq 2 ] || { echo "service-event reserved field: expected exit 2, got $rc" >&2; exit 1; }
    grep -q "invalid field 'ts=2000-01-01T00:00:00'" $TMPDIR/reserved.err || { echo "service-event reserved field: argument not named in error" >&2; exit 1; }
    # #934: a failed write (log dir cannot be created — Library is a
    # regular file) exits 1 keeping the stderr message, not 0.
    mkdir -p $TMPDIR/badhome
    touch $TMPDIR/badhome/Library
    rc=0
    HOME=$TMPDIR/badhome $out/bin/service-event test check.bad pid=42 2>$TMPDIR/write.err || rc=$?
    [ "$rc" -eq 1 ] || { echo "service-event failed write: expected exit 1, got $rc" >&2; exit 1; }
    grep -q "cannot write" $TMPDIR/write.err || { echo "service-event failed write: stderr message missing" >&2; exit 1; }
    # #935: a digit-like Unicode value (superscript two) is stored as a
    # string and the event is appended — no traceback, exit 0.
    HOME=$TMPDIR/unihome $out/bin/service-event test check.unicode 'count=²' pid=42
    # Assert on the parsed record, not the raw bytes (#1115): event.py
    # writes with json.dumps (default ensure_ascii=True), so the stored
    # line holds the ASCII escape \u00b2, never the literal character a
    # byte-level grep for '"count": "²"' looks for — that grep could never
    # match and reddened this package's build on main.
    ${python3}/bin/python3 - "$TMPDIR/unihome/Library/Logs/nix-services/events.jsonl" <<'EOF'
    import json, sys
    lines = open(sys.argv[1]).read().splitlines()
    assert len(lines) == 1, lines
    rec = json.loads(lines[0])
    assert rec["count"] == "²" and isinstance(rec["count"], str), rec
    assert rec["pid"] == 42 and isinstance(rec["pid"], int), rec
    EOF
    rc=0
    SERVICE_EVENTS_INTERVAL=0 $out/bin/service-monitor 2>/dev/null || rc=$?
    [ "$rc" -eq 2 ] || { echo "service-monitor zero interval: expected exit 2, got $rc" >&2; exit 1; }
    ${python3}/bin/python3 - "$log" <<'EOF'
    import json, sys
    lines = open(sys.argv[1]).read().splitlines()
    assert len(lines) == 1, lines
    rec = json.loads(lines[0])
    assert rec["source"] == "test" and rec["event"] == "check.one", rec
    assert rec["pid"] == 42 and rec["cause"] == "deploy", rec
    sys.path.insert(0, "${placeholder "out"}/lib/service-events")
    from event import rotate
    p = sys.argv[1] + ".rot"
    open(p, "w").write("x" * 100)
    assert rotate(p, 50, 2, copy_truncate=True) == 100
    import os
    assert os.path.getsize(p) == 0 and os.path.getsize(p + ".1") == 100
    assert rotate(p, 50, 2) == 0
    # #920: a drop older than the reconnect window must not produce a
    # conn.reconnect for an unrelated later start; a fresh drop must.
    import time
    import monitor
    captured = []
    monitor.append_event = lambda source, event, **f: captured.append((event, f))
    monitor.launchd_jobs = lambda: {}
    monitor.processes = lambda: {222: ("opencode", 1, "opencode")}
    monitor.sockets = lambda pids: {222: ({"4096"}, {"10.0.0.1:4096->10.0.0.2:5000"})}
    def tick_with_drop(age_s):
        captured.clear()
        m = monitor.Monitor()
        m.first = False
        m.last_stats = time.time()
        m.last_rotate = time.time()
        m.drops[("opencode", 1)] = (time.time() - age_s, 4, 111)
        m.tick()
        return [f for e, f in captured if e == "conn.reconnect"], m.drops
    stale, drops_after = tick_with_drop(259200)
    assert stale == [] and drops_after == {}, (stale, drops_after)
    fresh, _ = tick_with_drop(60)
    assert len(fresh) == 1 and fresh[0]["dropped"] == 4 and fresh[0]["previous_pid"] == 111, fresh
    EOF
  ''
