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
    EOF
  ''
