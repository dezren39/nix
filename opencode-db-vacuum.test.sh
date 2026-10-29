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

if [ "$failures" -ne 0 ]; then
  echo "$failures failure(s)" >&2
  exit 1
fi
echo "all opencode-db-vacuum core checks passed"
