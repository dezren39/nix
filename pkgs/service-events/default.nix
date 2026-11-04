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
    # #1208: an empty SOURCE or EVENT positional exits 2 naming the
    # position, and appends nothing -- the python check below still sees
    # exactly one line in the log.
    rc=0
    $out/bin/service-event "" check.bad 2>$TMPDIR/emptysrc.err || rc=$?
    [ "$rc" -eq 2 ] || { echo "service-event empty SOURCE: expected exit 2, got $rc" >&2; exit 1; }
    grep -q "SOURCE must not be empty" $TMPDIR/emptysrc.err || { echo "service-event empty SOURCE: position not named in error" >&2; exit 1; }
    rc=0
    $out/bin/service-event test "" 2>$TMPDIR/emptyevt.err || rc=$?
    [ "$rc" -eq 2 ] || { echo "service-event empty EVENT: expected exit 2, got $rc" >&2; exit 1; }
    grep -q "EVENT must not be empty" $TMPDIR/emptyevt.err || { echo "service-event empty EVENT: position not named in error" >&2; exit 1; }
    # #918: a bad SERVICE_EVENTS_INTERVAL exits 2 at startup naming the
    # variable and value, instead of crashing at import/sleep or busy-looping.
    rc=0
    SERVICE_EVENTS_INTERVAL=abc $out/bin/service-monitor 2>$TMPDIR/interval.err || rc=$?
    [ "$rc" -eq 2 ] || { echo "service-monitor bad interval: expected exit 2, got $rc" >&2; exit 1; }
    grep -q "invalid SERVICE_EVENTS_INTERVAL='abc'" $TMPDIR/interval.err || { echo "service-monitor bad interval: variable/value not named in error" >&2; exit 1; }
    # #1199: service-monitor takes no arguments -- --help prints usage to
    # stdout and exits 0, and any other argument exits 2 naming it, both
    # appending nothing (the python check below still sees exactly one
    # line in the log).
    $out/bin/service-monitor --help >$TMPDIR/help.out
    grep -q "usage: service-monitor" $TMPDIR/help.out || { echo "service-monitor --help: usage missing on stdout" >&2; exit 1; }
    rc=0
    $out/bin/service-monitor --bogus 2>$TMPDIR/bogus.err || rc=$?
    [ "$rc" -eq 2 ] || { echo "service-monitor unknown argument: expected exit 2, got $rc" >&2; exit 1; }
    grep -q "invalid argument '--bogus'" $TMPDIR/bogus.err || { echo "service-monitor unknown argument: argument not named in error" >&2; exit 1; }
    grep -q "usage: service-monitor" $TMPDIR/bogus.err || { echo "service-monitor unknown argument: usage missing on stderr" >&2; exit 1; }
    # #1205: service-event --help prints usage to stdout and exits 0,
    # and --help in the SOURCE position exits 2 naming it -- both
    # append nothing (the python check below still sees exactly one
    # line in the log; the second case used to append an event whose
    # source was the literal string "--help").
    $out/bin/service-event --help >$TMPDIR/event-help.out
    grep -q "usage: service-event" $TMPDIR/event-help.out || { echo "service-event --help: usage missing on stdout" >&2; exit 1; }
    rc=0
    $out/bin/service-event --help some.event 2>$TMPDIR/event-help2.err || rc=$?
    [ "$rc" -eq 2 ] || { echo "service-event --help as source: expected exit 2, got $rc" >&2; exit 1; }
    grep -q "invalid argument '--help'" $TMPDIR/event-help2.err || { echo "service-event --help as source: argument not named in error" >&2; exit 1; }
    # #919: reserved field names (ts/source/event) exit 2 naming the field,
    # and append nothing — the python check below still sees exactly one line.
    rc=0
    $out/bin/service-event test check.bad ts=2000-01-01T00:00:00 2>$TMPDIR/reserved.err || rc=$?
    [ "$rc" -eq 2 ] || { echo "service-event reserved field: expected exit 2, got $rc" >&2; exit 1; }
    grep -q "invalid field 'ts=2000-01-01T00:00:00'" $TMPDIR/reserved.err || { echo "service-event reserved field: argument not named in error" >&2; exit 1; }
    # #1127: a duplicated field name exits 2 naming the field, and
    # appends nothing — the python check below still sees exactly one
    # line in the log (the first value must not silently last-win).
    rc=0
    $out/bin/service-event test check.bad pid=1 pid=2 2>$TMPDIR/dup.err || rc=$?
    [ "$rc" -eq 2 ] || { echo "service-event duplicate field: expected exit 2, got $rc" >&2; exit 1; }
    grep -q "duplicate field 'pid=2'" $TMPDIR/dup.err || { echo "service-event duplicate field: field not named in error" >&2; exit 1; }
    grep -q "'pid' may be given only once" $TMPDIR/dup.err || { echo "service-event duplicate field: only-once rule not named in error" >&2; exit 1; }
    # #934: a failed write (log dir cannot be created — Library is a
    # regular file) exits 1 keeping the stderr message, not 0.
    mkdir -p $TMPDIR/badhome
    touch $TMPDIR/badhome/Library
    rc=0
    HOME=$TMPDIR/badhome $out/bin/service-event test check.bad pid=42 2>$TMPDIR/write.err || rc=$?
    [ "$rc" -eq 1 ] || { echo "service-event failed write: expected exit 1, got $rc" >&2; exit 1; }
    grep -q "cannot write" $TMPDIR/write.err || { echo "service-event failed write: stderr message missing" >&2; exit 1; }
    # #1200: the same bad HOME for service-monitor -- the startup
    # makedirs fails, so it exits 2 naming the log directory, with no
    # traceback.
    rc=0
    HOME=$TMPDIR/badhome $out/bin/service-monitor 2>$TMPDIR/makedirs.err || rc=$?
    [ "$rc" -eq 2 ] || { echo "service-monitor bad log dir: expected exit 2, got $rc" >&2; exit 1; }
    grep -q "cannot create log directory" $TMPDIR/makedirs.err || { echo "service-monitor bad log dir: directory not named in error" >&2; exit 1; }
    ! grep -q "Traceback" $TMPDIR/makedirs.err || { echo "service-monitor bad log dir: traceback leaked to stderr" >&2; exit 1; }
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
    # Bolt (issue #1464): classify() must return the same roles as the
    # unfiltered pattern set -- the hint pre-filter may only reject lines
    # that contain none of the watched substrings, never change a match.
    assert monitor.classify("/usr/bin/unrelated --serve") is None
    assert monitor.classify("/Applications/Nix Apps/OpenChamber.app/Contents/MacOS/OpenChamber") == "openchamber-desktop"
    assert monitor.classify("node /x/openchamber/packages/web/bin/cli.js serve") == "openchamber-server"
    assert monitor.classify("bun server/index.js") == "openchamber-dev"
    assert monitor.classify("/x/bin/opencode serve --service") == "opencode-service"
    assert monitor.classify("/x/bin/opencode serve") == "opencode"
    assert monitor.classify("/x/bin/mcpx daemon") == "mcpx-daemon"
    assert monitor.classify("/x/bin/mcpx client") is None
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
    # #1380: a failed sampler sample must not read as an empty one. A
    # tick whose samplers fail emits no fabricated exit/drop/close (or
    # start) events and retains jobs/procs/conns state; a genuinely
    # empty successful sample still reports the real exits.
    assert monitor.run([sys.executable, "-c", "print('hello'); raise SystemExit(3)"]) is None
    assert monitor.run(["/definitely/not/a/binary-xyz"]) is None
    assert monitor.run([sys.executable, "-c", "pass"]) == ""
    captured1380 = []
    monitor.append_event = lambda source, event, **f: captured1380.append((event, f))
    primed_jobs = {"org.nixos.mcpx": (4321, "0")}
    primed_procs = {4321: ("mcpx-daemon", 1, "mcpx")}
    conn1380 = "10.0.0.1:8080->10.0.0.2:5000"
    def prime1380(m):
        m.first = False
        m.last_stats = time.time()
        m.last_rotate = time.time()
        m.jobs = dict(primed_jobs)
        m.procs = dict(primed_procs)
        m.conns = {4321: {conn1380: time.time() - 100}}
        m.stats = {}
        m.drops = {}
    # All samplers failing: only monitor.error records, state retained.
    monitor.launchd_jobs = lambda: None
    monitor.processes = lambda: None
    monitor.sockets = lambda pids: None
    m1380 = monitor.Monitor()
    prime1380(m1380)
    m1380.tick()
    assert all(e == "monitor.error" for e, f in captured1380), captured1380
    assert m1380.jobs == primed_jobs and m1380.procs == primed_procs, (m1380.jobs, m1380.procs)
    assert set(m1380.conns[4321]) == {conn1380}, m1380.conns
    # The next good tick with unchanged data reports nothing (no starts).
    captured1380.clear()
    monitor.launchd_jobs = lambda: dict(primed_jobs)
    monitor.processes = lambda: dict(primed_procs)
    monitor.sockets = lambda pids: {4321: ({"8080"}, {conn1380})}
    m1380.tick()
    assert captured1380 == [], captured1380
    # A genuinely empty successful sample still reports the real exits.
    captured1380.clear()
    m1380b = monitor.Monitor()
    prime1380(m1380b)
    monitor.launchd_jobs = lambda: {}
    monitor.processes = lambda: {}
    monitor.sockets = lambda pids: {}
    m1380b.tick()
    exits1380 = [e for e, f in captured1380]
    assert "service.exit" in exits1380 and "process.exit" in exits1380 and "conn.drop" in exits1380, exits1380
    assert m1380b.jobs == {} and m1380b.procs == {}, (m1380b.jobs, m1380b.procs)
    # An lsof-only failure keeps the connection (no conn.close, no stats
    # churn); an empty socket sample for a live process closes it.
    captured1380.clear()
    m1380c = monitor.Monitor()
    prime1380(m1380c)
    monitor.launchd_jobs = lambda: dict(primed_jobs)
    monitor.processes = lambda: dict(primed_procs)
    monitor.sockets = lambda pids: None
    m1380c.tick()
    assert "conn.close" not in [e for e, f in captured1380], captured1380
    assert set(m1380c.conns[4321]) == {conn1380} and m1380c.stats == {}, (m1380c.conns, m1380c.stats)
    captured1380.clear()
    monitor.sockets = lambda pids: {4321: (set(), set())}
    m1380c.tick()
    assert "conn.close" in [e for e, f in captured1380], captured1380
    EOF
  ''
