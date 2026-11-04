#!/usr/bin/env bash
# Fixture-DB test for opencode-db-vacuum.sh's real VACUUM INTO + verify +
# swap core (#1088).
#
# The args suite pins the argument parser, the pids suite pins the
# opencode_pids() guard, and the args suite's only full runs are a
# --dry-run (exits before any vacuum) and the #1051 preflight refusal
# (refuses before VACUUM INTO runs). Nothing let the real script reach
# step 5: VACUUM INTO a compacted copy, verify integrity + per-table row
# counts, then swap it in (original kept as ${DB}.old, -wal/-shm
# removed). A swap or row-count regression would pass every other check
# and surface as a damaged or un-shrunk live database -- the step
# full-clean.sh runs unattended. This suite drives the real script
# against hand-built fixture SQLite DBs and pins that core, following
# the #1050 prune suite's precedent. Its --auto-vacuum case caught a
# real bug: the script set PRAGMA auto_vacuum in its own sqlite3
# connection, where the setting is lost on disconnect, so the flag was
# a no-op on any non-empty DB; the script now applies the pragma in
# the same connection as its VACUUM INTO.
#
# Hermetic: a stub `ps` that reports no processes is prepended to PATH
# (the prune suite's shadowing trick), so vacuum's running-process guard
# sees a stopped opencode on any machine. Fixture DBs only -- never a
# real user database.
#
# Run: ./opencode-db-vacuum.test.sh
# It is run by the flake check `opencode-db-vacuum-tests`.
set -u

cd "$(dirname "$0")" || exit

HOME="$(mktemp -d)"
export HOME
trap 'rm -rf "$HOME"' EXIT

failures=0
fail() { echo "FAIL: $1" >&2; failures=$((failures + 1)); }
ok() { echo "ok: $1"; }
# ok_if_clean <failures-before> <message>: print ok only if the case
# added no failures.
ok_if_clean() { [ "$failures" -eq "$1" ] && ok "$2"; return 0; }

command -v sqlite3 >/dev/null || { echo "sqlite3 required" >&2; exit 1; }

# Stub ps: no processes at all, so opencode_pids() finds nothing running.
stub="$HOME/stubbin"; mkdir -p "$stub"
printf '#!/bin/sh\nexit 0\n' >"$stub/ps"; chmod +x "$stub/ps"
PATH="$stub:$PATH"; export PATH

VACUUM=./opencode-db-vacuum.sh

# Fixture layout: the four tables vacuum's verification counts.
# session/message/part hold 500 rows each. event holds 500 rows with
# ~512-char payloads, 490 of them deleted before the run, leaving 10
# rows and a real freelist -- so a real run has something to reclaim
# (freelist_count 0 makes the script stop before vacuuming) and the
# swapped file must be measurably smaller. Expected counts after any
# successful vacuum: 500/500/500/10.
mk_fixture() { # $1 = db path
  sqlite3 "$1" <<'SQL'
CREATE TABLE session(id TEXT PRIMARY KEY);
CREATE TABLE message(id TEXT PRIMARY KEY);
CREATE TABLE part(id TEXT PRIMARY KEY);
CREATE TABLE event(id TEXT PRIMARY KEY, data TEXT);
WITH RECURSIVE c(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM c WHERE n < 500)
INSERT INTO session SELECT 'ses_' || n FROM c;
WITH RECURSIVE c(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM c WHERE n < 500)
INSERT INTO message SELECT 'msg_' || n FROM c;
WITH RECURSIVE c(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM c WHERE n < 500)
INSERT INTO part SELECT 'prt_' || n FROM c;
WITH RECURSIVE c(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM c WHERE n < 500)
INSERT INTO event SELECT 'ev_' || n, hex(randomblob(256)) FROM c;
DELETE FROM event WHERE CAST(substr(id, 4) AS INTEGER) > 10;
SQL
}

count_of() { sqlite3 "$1" "SELECT COUNT(*) FROM $2;"; }
size_of() { wc -c <"$1" | tr -d ' '; }
counts_ok() { # $1 = db path
  [ "$(count_of "$1" session)" = "500" ] && [ "$(count_of "$1" message)" = "500" ] \
    && [ "$(count_of "$1" part)" = "500" ] && [ "$(count_of "$1" event)" = "10" ]
}
freelist_of() { sqlite3 "$1" 'PRAGMA freelist_count;'; }

fx="$HOME/fx"; mkdir -p "$fx"

# Fixture sanity: the freelist must be non-trivial, or every case below
# would silently exercise the script's nothing-to-reclaim early exit.
db="$fx/sanity.db"; mk_fixture "$db"
[ "$(freelist_of "$db")" -gt 0 ] || fail "fixture freelist_count is 0; real-vacuum cases would not run"
counts_ok "$db" || fail "fixture counts are not 500/500/500/10 before any run"
ok_if_clean 0 "fixture has a real freelist and counts 500/500/500/10"

# --- Real run: VACUUM INTO + verify + swap, backup kept. ---------------
before=$failures
db="$fx/happy.db"; mk_fixture "$db"
orig_size="$(size_of "$db")"
out="$(bash "$VACUUM" --db "$db" -y 2>&1)"; rc=$?
if [ "$rc" -ne 0 ]; then
  fail "real run exited $rc: $out"
else
  new_size="$(size_of "$db")"
  [ "$new_size" -lt "$orig_size" ] || fail "swapped DB did not shrink: $orig_size -> $new_size"
  counts_ok "$db" || fail "row counts after swap are not 500/500/500/10"
  [ -f "${db}.old" ] || fail "backup ${db}.old missing after swap"
  [ -f "${db}.old" ] && [ "$(size_of "${db}.old")" = "$orig_size" ] \
    || fail "backup ${db}.old size $(size_of "${db}.old" 2>/dev/null), expected original $orig_size"
  integ="$(sqlite3 "$db" 'PRAGMA integrity_check;')"
  [ "$integ" = "ok" ] || fail "integrity_check after swap: $integ"
  [ ! -e "${db}.compact" ] || fail "compacted copy left behind after swap"
  ok_if_clean "$before" "real run shrinks the DB, preserves 500/500/500/10, keeps .old at the original size, integrity ok"
fi

# --- Existing .old refusal leaves the DB byte-identical. ---------------
before=$failures
db="$fx/old.db"; mk_fixture "$db"
cp "$db" "$fx/old.before"
touch "${db}.old"
out="$(bash "$VACUUM" --db "$db" -y 2>&1)"; rc=$?
[ "$rc" -ne 0 ] || fail "existing-.old run exited 0, expected refusal: $out"
cmp -s "$db" "$fx/old.before" || fail "existing-.old refusal changed the DB"
[ ! -e "${db}.compact" ] || fail "existing-.old refusal created a compacted copy"
ok_if_clean "$before" "existing .old refuses, DB byte-identical, no compacted copy created"

# --- Existing .compact refusal leaves the DB byte-identical. -----------
before=$failures
db="$fx/compact.db"; mk_fixture "$db"
cp "$db" "$fx/compact.before"
printf 'sentinel\n' >"${db}.compact"
out="$(bash "$VACUUM" --db "$db" -y 2>&1)"; rc=$?
[ "$rc" -ne 0 ] || fail "existing-.compact run exited 0, expected refusal: $out"
cmp -s "$db" "$fx/compact.before" || fail "existing-.compact refusal changed the DB"
[ "$(cat "${db}.compact")" = "sentinel" ] || fail "existing-.compact refusal overwrote the placeholder"
ok_if_clean "$before" "existing .compact refuses, DB byte-identical, placeholder untouched"

# --- --no-backup removes .old after the swap. ---------------------------
before=$failures
db="$fx/nobackup.db"; mk_fixture "$db"
orig_size="$(size_of "$db")"
out="$(bash "$VACUUM" --db "$db" -y --no-backup 2>&1)"; rc=$?
if [ "$rc" -ne 0 ]; then
  fail "--no-backup run exited $rc: $out"
else
  [ ! -e "${db}.old" ] || fail "--no-backup left ${db}.old behind"
  counts_ok "$db" || fail "row counts after --no-backup swap are not 500/500/500/10"
  [ "$(size_of "$db")" -lt "$orig_size" ] || fail "--no-backup swap did not shrink the DB"
  ok_if_clean "$before" "--no-backup swaps, shrinks, preserves counts, and removes .old"
fi

# --- Custom --into path is the copy that gets swapped in. ---------------
before=$failures
db="$fx/custom.db"; mk_fixture "$db"
into="$fx/custom.compact"
out="$(bash "$VACUUM" --db "$db" -y --into "$into" 2>&1)"; rc=$?
if [ "$rc" -ne 0 ]; then
  fail "custom --into run exited $rc: $out"
else
  counts_ok "$db" || fail "row counts after custom --into swap are not 500/500/500/10"
  [ -f "${db}.old" ] || fail "custom --into run missing backup ${db}.old"
  [ ! -e "$into" ] || fail "custom --into copy was not moved into place"
  [ ! -e "${db}.compact" ] || fail "custom --into run also wrote the default .compact path"
  ok_if_clean "$before" "custom --into copy is verified and swapped in; default .compact path unused"
fi

# --- --auto-vacuum reads back as INCREMENTAL on the swapped DB. ---------
before=$failures
db="$fx/autovac.db"; mk_fixture "$db"
out="$(bash "$VACUUM" --db "$db" -y --auto-vacuum 2>&1)"; rc=$?
if [ "$rc" -ne 0 ]; then
  fail "--auto-vacuum run exited $rc: $out"
else
  av="$(sqlite3 "$db" 'PRAGMA auto_vacuum;')"
  [ "$av" = "2" ] || fail "auto_vacuum on swapped DB is $av, expected 2 (INCREMENTAL)"
  counts_ok "$db" || fail "row counts after --auto-vacuum swap are not 500/500/500/10"
  ok_if_clean "$before" "--auto-vacuum bakes INCREMENTAL (2) into the swapped DB, counts preserved"
fi

# --- Failed row-count queries abort before the swap (#1386). ----------
# The verify loop used to turn a failed COUNT(*) on the original into
# "skip" and continue silently: if all four counts failed, zero row
# counts were verified and the swap proceeded at exit 0 (a failed count
# on the compacted copy already died, via the mismatch check). A shim
# sqlite3 that fails only COUNT(*) queries against one side's
# read-only URI pins the fix: either side failing must abort before
# the swap, naming the table and the side, with the original untouched.
real_sqlite3="$(command -v sqlite3)"
mk_count_fail_shim() { # $1 = shim dir, $2 = URI fragment whose COUNTs fail
  mkdir -p "$1"
  # The shebang must name a real interpreter: /usr/bin/env does not exist in
  # the nix build sandbox (#978, #1408), so the shim died with "bad
  # interpreter" and both count-failure scenarios failed for the wrong
  # reason. patchShebangs cannot help here because the file only exists at
  # run time. Expand bash's resolved path when the shim is generated.
  cat >"$1/sqlite3" <<EOF
#!$(command -v bash)
for a in "\$@"; do
  case "\$a" in
    *"SELECT COUNT(*) FROM"*)
      for b in "\$@"; do
        case "\$b" in
          *"$2"*) echo 'Error: simulated count failure' >&2; exit 1 ;;
        esac
      done
      ;;
  esac
done
exec $real_sqlite3 "\$@"
EOF
  chmod +x "$1/sqlite3"
}

before=$failures
db="$fx/countfail-orig.db"; mk_fixture "$db"
cp "$db" "$fx/countfail-orig.before"
shimdir="$HOME/countfail-orig-shim"
mk_count_fail_shim "$shimdir" "$(basename "$db")?mode=ro"
out="$(PATH="$shimdir:$PATH" bash "$VACUUM" --db "$db" -y 2>&1)"; rc=$?
[ "$rc" -ne 0 ] || fail "original-side count failure exited 0, expected abort before swap: $out"
case "$out" in
  *"row count query failed for session on the original"*) ;;
  *) fail "original-side count failure did not name table and side: $out" ;;
esac
cmp -s "$db" "$fx/countfail-orig.before" || fail "original-side count failure changed the DB (swap happened?)"
[ ! -e "${db}.old" ] || fail "original-side count failure created ${db}.old (swap happened)"
ok_if_clean "$before" "failed COUNT(*) on the original aborts before the swap, naming table and side, original untouched"

before=$failures
db="$fx/countfail-compact.db"; mk_fixture "$db"
cp "$db" "$fx/countfail-compact.before"
shimdir="$HOME/countfail-compact-shim"
mk_count_fail_shim "$shimdir" ".compact?mode=ro"
out="$(PATH="$shimdir:$PATH" bash "$VACUUM" --db "$db" -y 2>&1)"; rc=$?
[ "$rc" -ne 0 ] || fail "compacted-side count failure exited 0, expected abort before swap: $out"
case "$out" in
  *"row count query failed for session on the compacted copy"*) ;;
  *) fail "compacted-side count failure did not name table and side: $out" ;;
esac
cmp -s "$db" "$fx/countfail-compact.before" || fail "compacted-side count failure changed the DB (swap happened?)"
[ ! -e "${db}.old" ] || fail "compacted-side count failure created ${db}.old (swap happened)"
ok_if_clean "$before" "failed COUNT(*) on the compacted copy aborts before the swap, naming table and side, original untouched"


# --- Many-line integrity_check must not SIGPIPE the capture (#1460). --
# Both integrity captures used to read through `| head -1` under
# pipefail: integrity_check emits one line per problem, so on a damaged
# DB the output exceeds the pipe buffer, head exits, sqlite3 dies with
# SIGPIPE, and the script aborted with exit 141 and no verdict. A shim
# sqlite3 that floods integrity_check with 5000 lines pins the fix
# (full capture + in-shell first line): each gate must reach its die
# message at exit 1. real_sqlite3 is defined by the #1386 section above.
mk_integrity_flood_shim() { # $1 = shim dir, $2 = all | compact
  mkdir -p "$1"
  cat >"$1/sqlite3" <<EOF
#!$(command -v bash)
for a in "\$@"; do
  case "\$a" in
    *integrity_check*)
      if [ "$2" = compact ]; then
        hit=0
        for b in "\$@"; do case "\$b" in *.compact*) hit=1 ;; esac; done
        [ "\$hit" -eq 1 ] || exec $real_sqlite3 "\$@"
      fi
      i=1; while [ \$i -le 5000 ]; do echo "row 999\$i missing from index idx_corrupt"; i=\$((i + 1)); done
      exit 0 ;;
  esac
done
exec $real_sqlite3 "\$@"
EOF
  chmod +x "$1/sqlite3"
}

before=$failures
db="$fx/icflood-pre.db"; mk_fixture "$db"
cp "$db" "$fx/icflood-pre.before"
shimdir="$HOME/icflood-pre-shim"
mk_integrity_flood_shim "$shimdir" all
out="$(PATH="$shimdir:$PATH" bash "$VACUUM" --db "$db" -y 2>&1)"; rc=$?
[ "$rc" -eq 1 ] || fail "pre-vacuum integrity flood exited $rc, expected 1 (141 is the SIGPIPE bug): $out"
case "$out" in
  *"integrity check failed -- do NOT vacuum"*) ;;
  *) fail "pre-vacuum integrity flood did not print the corruption verdict: $out" ;;
esac
cmp -s "$db" "$fx/icflood-pre.before" || fail "pre-vacuum integrity flood changed the DB"
[ ! -e "${db}.compact" ] || fail "pre-vacuum integrity flood created a compacted copy"
ok_if_clean "$before" "many-line integrity_check at the pre-vacuum gate dies at exit 1 with the corruption verdict, DB untouched"

before=$failures
db="$fx/icflood-compact.db"; mk_fixture "$db"
cp "$db" "$fx/icflood-compact.before"
shimdir="$HOME/icflood-compact-shim"
mk_integrity_flood_shim "$shimdir" compact
out="$(PATH="$shimdir:$PATH" bash "$VACUUM" --db "$db" -y 2>&1)"; rc=$?
[ "$rc" -eq 1 ] || fail "compacted-copy integrity flood exited $rc, expected 1 (141 is the SIGPIPE bug): $out"
case "$out" in
  *"compacted copy failed integrity check"*) ;;
  *) fail "compacted-copy integrity flood did not print the verification verdict: $out" ;;
esac
cmp -s "$db" "$fx/icflood-compact.before" || fail "compacted-copy integrity flood changed the original DB (swap happened?)"
[ ! -e "${db}.old" ] || fail "compacted-copy integrity flood created ${db}.old (swap happened)"
ok_if_clean "$before" "many-line integrity_check on the compacted copy dies at exit 1 with the verification verdict, original untouched"

if [ "$failures" -ne 0 ]; then
  echo "$failures failure(s)" >&2
  exit 1
fi
echo "all opencode-db-vacuum core checks passed"
