#!/usr/bin/env bash
# Fixture-DB test for opencode-db-prune.sh's destructive core (#1050).
#
# The args suite pins the argument parser and the pids suite pins the
# opencode_pids() guard, but nothing exercised the delete itself: the
# eligibility CTE (keyed/ranked, opencode-db-prune.sh), the batched
# delete, the --archive dump, or the --restore-session round-trip. A CTE
# regression (dropping the owner_id exclusion, or the newest-per-entity
# seq < max_seq rule) would silently delete rows that must be kept from
# the live database this script prunes unattended -- and nothing would
# fail. This suite drives the real script against hand-built fixture
# SQLite DBs and pins the exact row sets at every step.
#
# Hermetic: a stub `ps` that reports no processes is prepended to PATH
# (the pids suite's shadowing trick), so prune's check_not_running sees
# a stopped opencode on any machine. Fixture DBs only -- never a real
# user database.
#
# Run: ./opencode-db-prune.test.sh
# It is run by the flake check `opencode-db-prune-tests`.
set -u

cd "$(dirname "$0")" || exit

HOME="$(mktemp -d)"
export HOME
trap 'rm -rf "$HOME"' EXIT

failures=0
fail() { echo "FAIL: $1" >&2; failures=$((failures + 1)); }
ok() { echo "ok: $1"; }

command -v sqlite3 >/dev/null || { echo "sqlite3 required" >&2; exit 1; }

# Stub ps: no processes at all, so opencode_pids() finds nothing running.
stub="$HOME/stubbin"; mkdir -p "$stub"
printf '#!/bin/sh\nexit 0\n' >"$stub/ps"; chmod +x "$stub/ps"
PATH="$stub:$PATH"; export PATH

PRUNE=./opencode-db-prune.sh
NOW_MS=$(( $(date +%s) * 1000 ))

# Fixture layout (seqs are 0-based, as in the real event table):
#   ses_old   root, old. msg_1 snapshots seq 0,1,2 + non-snapshot seq 3.
#   ses_child child of ses_old, old. msg_2 snapshots seq 0,1.
#   ses_owned root, old, event_sequence.owner_id set. msg_3 seq 0,1.
#   ses_new   root, active an hour ago (inside --protect-days 30, well
#             clear of the --protect-days 0 cutoff boundary). msg_4 seq 0,1.
# Eligible under --protect-days 0: old seq 0,1 + child seq 0 + new seq 0 = 4.
# Under --protect-days 30 the recent session drops out: 3.
# Under --protect-roots only child sessions are eligible: 1.
mk_fixture() { # $1 = db path
  sqlite3 "$1" <<SQL
CREATE TABLE session(id TEXT PRIMARY KEY, parent_id TEXT, time_updated INTEGER);
CREATE TABLE message(id TEXT);
CREATE TABLE part(id TEXT);
CREATE TABLE event(id TEXT, aggregate_id TEXT, type TEXT, seq INTEGER, data TEXT);
CREATE TABLE event_sequence(aggregate_id TEXT, seq INTEGER, owner_id TEXT);
INSERT INTO session VALUES ('ses_old', NULL, 1000);
INSERT INTO session VALUES ('ses_child', 'ses_old', 1000);
INSERT INTO session VALUES ('ses_owned', NULL, 1000);
INSERT INTO session VALUES ('ses_new', NULL, $(( NOW_MS - 3600000 )));
INSERT INTO event VALUES ('ev_o0','ses_old','message.updated.1',0,'{"info":{"id":"msg_1"}}');
INSERT INTO event VALUES ('ev_o1','ses_old','message.updated.1',1,'{"info":{"id":"msg_1"}}');
INSERT INTO event VALUES ('ev_o2','ses_old','message.updated.1',2,'{"info":{"id":"msg_1"}}');
INSERT INTO event VALUES ('ev_o3','ses_old','session.idle.1',3,'{}');
INSERT INTO event VALUES ('ev_c0','ses_child','message.updated.1',0,'{"info":{"id":"msg_2"}}');
INSERT INTO event VALUES ('ev_c1','ses_child','message.updated.1',1,'{"info":{"id":"msg_2"}}');
INSERT INTO event VALUES ('ev_w0','ses_owned','message.updated.1',0,'{"info":{"id":"msg_3"}}');
INSERT INTO event VALUES ('ev_w1','ses_owned','message.updated.1',1,'{"info":{"id":"msg_3"}}');
INSERT INTO event VALUES ('ev_n0','ses_new','message.updated.1',0,'{"info":{"id":"msg_4"}}');
INSERT INTO event VALUES ('ev_n1','ses_new','message.updated.1',1,'{"info":{"id":"msg_4"}}');
INSERT INTO event_sequence VALUES ('ses_old',3,NULL);
INSERT INTO event_sequence VALUES ('ses_child',1,NULL);
INSERT INTO event_sequence VALUES ('ses_owned',1,'user_1');
INSERT INTO event_sequence VALUES ('ses_new',1,NULL);
SQL
}

seqs_of() { sqlite3 "$1" "SELECT group_concat(seq) FROM (SELECT seq FROM event WHERE aggregate_id='$2' ORDER BY seq);"; }
count_of() { sqlite3 "$1" "SELECT COUNT(*) FROM event;"; }

# dry_total <db> <extra prune flags...> -- prints the TOTAL row count
# from a dry run, or empty on failure.
dry_total() {
  local db="$1"; shift
  bash "$PRUNE" --db "$db" "$@" 2>&1 \
    | sed -n 's/^  TOTAL[[:space:]]*\([0-9][0-9]*\)[[:space:]].*/\1/p'
}

fx="$HOME/fx"; mkdir -p "$fx"

# --- Dry-run eligibility is exact, and a dry run changes nothing. ------
db="$fx/dry.db"; mk_fixture "$db"
t="$(dry_total "$db" --protect-days 0)"
[ "$t" = "4" ] || fail "dry run --protect-days 0: TOTAL=$t, expected 4"
[ "$(count_of "$db")" = "10" ] || fail "dry run changed the fixture (event count $(count_of "$db"), expected 10)"
[ "$t" = "4" ] && [ "$(count_of "$db")" = "10" ] && ok "dry run --protect-days 0 reports exactly 4 eligible, DB unchanged"

t="$(dry_total "$db" --protect-days 30)"
[ "$t" = "3" ] || fail "dry run --protect-days 30: TOTAL=$t, expected 3 (recent session protected)"
[ "$t" = "3" ] && ok "dry run --protect-days 30 excludes the recent session (3 eligible)"

t="$(dry_total "$db" --protect-days 0 --protect-roots)"
[ "$t" = "1" ] || fail "dry run --protect-roots: TOTAL=$t, expected 1 (only the child session)"
[ "$t" = "1" ] && ok "dry run --protect-roots prunes only child sessions (1 eligible)"

# --- Apply with --archive deletes exactly the eligible set. ------------
db="$fx/apply.db"; mk_fixture "$db"
arch="$fx/arch.ndjson.gz"
out="$(bash "$PRUNE" --db "$db" --protect-days 0 --apply -y --no-backup --archive "$arch" 2>&1)"; rc=$?
if [ "$rc" -ne 0 ]; then
  fail "apply on fixture exited $rc: $out"
else
  [ "$(count_of "$db")" = "6" ] || fail "apply left $(count_of "$db") event rows, expected 6"
  [ "$(seqs_of "$db" ses_old)" = "2,3" ] || fail "ses_old seqs after apply: $(seqs_of "$db" ses_old), expected 2,3 (newest + non-snapshot kept)"
  [ "$(seqs_of "$db" ses_child)" = "1" ] || fail "ses_child seqs after apply: $(seqs_of "$db" ses_child), expected 1"
  [ "$(seqs_of "$db" ses_owned)" = "0,1" ] || fail "ses_owned seqs after apply: $(seqs_of "$db" ses_owned), expected 0,1 (owner_id aggregates excluded)"
  [ "$(seqs_of "$db" ses_new)" = "1" ] || fail "ses_new seqs after apply: $(seqs_of "$db" ses_new), expected 1"
  integ="$(sqlite3 "$db" 'PRAGMA integrity_check;')"
  [ "$integ" = "ok" ] || fail "integrity_check after apply: $integ"
  [ -f "$arch" ] || fail "archive $arch was not written"
  if [ "$(count_of "$db")" = "6" ] && [ "$(seqs_of "$db" ses_old)" = "2,3" ] \
     && [ "$(seqs_of "$db" ses_child)" = "1" ] && [ "$(seqs_of "$db" ses_owned)" = "0,1" ] \
     && [ "$(seqs_of "$db" ses_new)" = "1" ] && [ "$integ" = "ok" ] && [ -f "$arch" ]; then
    ok "apply deletes only the 4 superseded snapshots; newest, non-snapshot, sync-owned rows survive; integrity ok"
  fi

  # --- Restore round-trips the archived rows with original seqs. -------
  out="$(bash "$PRUNE" --db "$db" --restore-session ses_old --from "$arch" --apply 2>&1)"; rc=$?
  if [ "$rc" -ne 0 ]; then
    fail "restore-session ses_old exited $rc: $out"
  elif [ "$(seqs_of "$db" ses_old)" != "0,1,2,3" ]; then
    fail "ses_old seqs after restore: $(seqs_of "$db" ses_old), expected 0,1,2,3"
  else
    ok "restore-session re-inserts ses_old's archived rows with original seqs (0,1,2,3)"
  fi
fi

if [ "$failures" -ne 0 ]; then
  echo "$failures failure(s)" >&2
  exit 1
fi
echo "all opencode-db-prune core checks passed"
