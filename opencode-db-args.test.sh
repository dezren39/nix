#!/usr/bin/env bash
# Regression test for the argument parsers of opencode-db-vacuum.sh and
# opencode-db-prune.sh (#941, the #930 pattern; #1112 repeats/conflicts).
#
# A value-taking option must reject an empty or flag-shaped value, dying
# with a message that names the option — otherwise the next option is
# swallowed as the value and never parsed: "opencode-db-vacuum.sh --into
# --dry-run" used to run a REAL vacuum against the live database, because
# --dry-run became the --into path. The missing-value case (option last
# on the line) was already guarded by ${2:?}; this test pins both cases
# for every value-taking option in both scripts, plus controls showing
# valid values still parse.
#
# #1129: restore mode (--restore-session) silently ignored the prune-only
# options --backup/--no-backup/--protect-days/--protect-roots/--batch
# (only --archive was rejected). The restore branch now rejects each of
# them naming the option; the pins are below, after the #1112 checks.
#
# Run: ./opencode-db-args.test.sh
# It is run by the flake check `opencode-db-args-tests`.
set -u

cd "$(dirname "$0")" || exit

HOME="$(mktemp -d)"
export HOME
trap 'rm -rf "$HOME"' EXIT

failures=0
fail() { echo "FAIL: $1" >&2; failures=$((failures + 1)); }
ok() { echo "ok: $1"; }

# check_rejected <script> <option> <flag-shaped value...>
# The invocation must exit non-zero before doing any work, with stderr
# naming the option.
check_rejected() {
  local script="$1" opt="$2"; shift 2
  local out rc
  out="$(bash "$script" "$opt" "$@" 2>&1)"; rc=$?
  if [ "$rc" -eq 0 ]; then
    fail "$script $opt $*: exited 0"
  elif ! printf '%s' "$out" | grep -q -- "$opt"; then
    fail "$script $opt $*: error does not name the option: $out"
  else
    ok "$script $opt $* rejected, naming $opt"
  fi
}

VAC=./opencode-db-vacuum.sh
PRUNE=./opencode-db-prune.sh

# Flag-shaped values: the swallowed flag is the dangerous case (#930).
for opt in --db --into; do
  check_rejected "$VAC" "$opt" --dry-run
  check_rejected "$VAC" "$opt"        # no value at all
  check_rejected "$VAC" "$opt" ""     # empty value
done
for opt in --db --protect-days --backup --archive --restore-session --from --batch; do
  check_rejected "$PRUNE" "$opt" --apply
  check_rejected "$PRUNE" "$opt"      # no value at all
  check_rejected "$PRUNE" "$opt" ""   # empty value
done

# Repeated value options (#1112, the adopt #1103 pattern): the last value
# used to silently win, so a duplicated --db redirected the destructive
# operation to a different database with no error. Every value-taking
# option in both scripts must reject a repeat at parse time, naming the
# option with "may be given only once", before any work happens.
check_repeated() {
  local script="$1" opt="$2"; shift 2
  local out rc
  out="$(bash "$script" "$@" 2>&1)"; rc=$?
  if [ "$rc" -eq 0 ]; then
    fail "$script $*: repeated $opt exited 0"
  elif ! printf '%s' "$out" | grep -q -- "$opt may be given only once"; then
    fail "$script $*: repeated $opt error does not name the repetition: $out"
  else
    ok "$script repeated $opt rejected at parse time"
  fi
}
check_repeated "$VAC" --db --db /tmp/a.db --db /tmp/b.db
check_repeated "$VAC" --into --into /tmp/a.out --into /tmp/b.out
check_repeated "$PRUNE" --db --db /tmp/a.db --db /tmp/b.db
check_repeated "$PRUNE" --protect-days --protect-days 1 --protect-days 2
check_repeated "$PRUNE" --backup --backup /tmp/a.bak --backup /tmp/b.bak
check_repeated "$PRUNE" --archive --archive /tmp/a.arc --archive /tmp/b.arc
check_repeated "$PRUNE" --restore-session --restore-session ses_a --restore-session ses_b
check_repeated "$PRUNE" --from --from /tmp/a.src --from /tmp/b.src
check_repeated "$PRUNE" --batch --batch 1 --batch 2

# Prune --backup + --no-backup (#1112): in either order the combination
# used to silently produce no backup at all, ignoring the explicit
# --backup. It must be rejected at parse time naming the combination.
check_backup_conflict() {
  local out rc
  out="$(bash "$PRUNE" "$@" 2>&1)"; rc=$?
  if [ "$rc" -eq 0 ]; then
    fail "prune $*: --backup + --no-backup exited 0"
  elif ! printf '%s' "$out" | grep -q -- "--backup and --no-backup may not be combined"; then
    fail "prune $*: --backup + --no-backup error does not name the combination: $out"
  else
    ok "prune $* rejected (--backup + --no-backup)"
  fi
}
check_backup_conflict --backup /tmp/a.bak --no-backup
check_backup_conflict --no-backup --backup /tmp/a.bak

# Restore mode rejects the prune-only options (#1129): each was parsed
# and then silently dropped by --restore-session — worst, --backup on a
# restore --apply run produced no backup and no warning. Each must die
# at the restore branch (exit 1 via die, before any restore work — the
# --db/--from paths here do not exist, proving the rejection precedes
# restore preflight), naming the option and that it does not apply to
# --restore-session, mirroring the existing --archive rejection.
check_restore_rejected() {
  local opt="$1"; shift
  local out rc
  out="$(bash "$PRUNE" --db /tmp/a.db --restore-session ses_a --from /tmp/a.src "$@" 2>&1)"; rc=$?
  if [ "$rc" -eq 0 ]; then
    fail "prune restore $*: $opt in restore mode exited 0"
  elif ! printf '%s' "$out" | grep -q -- "$opt is a prune option; it does not apply to --restore-session"; then
    fail "prune restore $*: error does not name $opt as a prune option not applying to --restore-session: $out"
  else
    ok "prune restore $* rejected, naming $opt"
  fi
}
check_restore_rejected --backup --backup /tmp/a.bak
check_restore_rejected --no-backup --no-backup
check_restore_rejected --protect-days --protect-days 1
check_restore_rejected --protect-roots --protect-roots
check_restore_rejected --batch --batch 1

# Controls for the new guards: a single --backup, a lone --no-backup, and
# single uses of the guarded options still parse (an unknown option after
# them is reported as unknown, proving parsing continued past them).
check_parses_past() {
  local script="$1"; shift
  local out rc
  out="$(bash "$script" "$@" --bogus 2>&1)"; rc=$?
  if [ "$rc" -eq 0 ] || ! printf '%s' "$out" | grep -q "unknown option: --bogus"; then
    fail "$script $*: expected 'unknown option: --bogus', got rc=$rc: $out"
  else
    ok "$script $* parses (no false repeat/conflict rejection)"
  fi
}
check_parses_past "$PRUNE" --backup /tmp/a.bak
check_parses_past "$PRUNE" --no-backup
check_parses_past "$PRUNE" --protect-days 7 --archive /tmp/a.arc --batch 10
check_parses_past "$VAC" --into /tmp/a.out

# Controls: a valid value is consumed and parsing continues to the next
# token (an unknown option after it must be reported as unknown, proving
# the value was not confused with a flag and the flag was not swallowed).
check_consumes_value() {
  local script="$1"
  local out rc
  out="$(bash "$script" --db /tmp/args-test-nonexistent.db --bogus 2>&1)"; rc=$?
  if [ "$rc" -eq 0 ] || ! printf '%s' "$out" | grep -q "unknown option: --bogus"; then
    fail "$script --db <path> --bogus: expected 'unknown option: --bogus', got rc=$rc: $out"
  else
    ok "$script --db consumes a valid value"
  fi
}
check_consumes_value "$VAC"
check_consumes_value "$PRUNE"

# Control: vacuum happy path — a real scratch DB in --dry-run mode exits
# 0 and changes nothing.
if command -v sqlite3 >/dev/null; then
  scratch="$HOME/scratch.db"
  sqlite3 "$scratch" "create table t(x); insert into t values(1);"
  out="$(bash "$VAC" --db "$scratch" --dry-run 2>&1)"; rc=$?
  if [ "$rc" -ne 0 ]; then
    fail "vacuum --dry-run on scratch DB exited $rc: $out"
  else
    ok "vacuum --dry-run happy path"
  fi
fi

# ---------------------------------------------------------------------------
# Preflight schema guard (#1051): prune's guard must assert every table the
# run later assumes (post-delete verification counts session/message/part/
# event_sequence; the eligibility SQL joins session on id/parent_id/
# time_updated), and vacuum must refuse up front for the tables its swap
# verification counts -- before any delete or compacted copy. A drifted DB
# used to be pruned first and fail mid-verification.
# ---------------------------------------------------------------------------
if command -v sqlite3 >/dev/null; then
  fx="$HOME/fx1051"; mkdir -p "$fx"

  # Full-schema fixture: one old session, 3 snapshots of msg_1 (seq 1-3,
  # so exactly seq 1-2 are eligible under --protect-days 0).
  mk_fixture() { # $1 = db path
    sqlite3 "$1" <<'SQL'
CREATE TABLE session(id TEXT PRIMARY KEY, parent_id TEXT, time_updated INTEGER);
CREATE TABLE message(id TEXT);
CREATE TABLE part(id TEXT);
CREATE TABLE event(aggregate_id TEXT, type TEXT, seq INTEGER, data TEXT);
CREATE TABLE event_sequence(aggregate_id TEXT, seq INTEGER, owner_id TEXT);
INSERT INTO session VALUES ('ses_old', NULL, 1000);
INSERT INTO event VALUES ('ses_old','message.updated.1',1,'{"info":{"id":"msg_1"}}');
INSERT INTO event VALUES ('ses_old','message.updated.1',2,'{"info":{"id":"msg_1"}}');
INSERT INTO event VALUES ('ses_old','message.updated.1',3,'{"info":{"id":"msg_1"}}');
INSERT INTO event_sequence VALUES ('ses_old',1,NULL),('ses_old',2,NULL),('ses_old',3,NULL);
SQL
  }
  event_count() { sqlite3 "$1" "SELECT COUNT(*) FROM event;"; }

  # check_prune_refused <db> <expected message fragment>
  check_prune_refused() {
    local db="$1" want="$2" out rc
    out="$(bash "$PRUNE" --db "$db" --protect-days 0 --apply -y --no-backup 2>&1)"; rc=$?
    if [ "$rc" -eq 0 ]; then
      fail "prune on $db: exited 0, expected preflight refusal"
    elif ! printf '%s' "$out" | grep -q -- "$want"; then
      fail "prune on $db: refusal does not name '$want': $out"
    elif [ "$(event_count "$db")" != "3" ]; then
      fail "prune on $db: event rows changed despite refusal"
    else
      ok "prune refuses $db preflight ($want), rows unchanged"
    fi
  }

  db="$fx/missing-message.db"; mk_fixture "$db"; sqlite3 "$db" "DROP TABLE message;"
  check_prune_refused "$db" "message table missing"

  db="$fx/missing-part.db"; mk_fixture "$db"; sqlite3 "$db" "DROP TABLE part;"
  check_prune_refused "$db" "part table missing"

  db="$fx/missing-parent.db"; mk_fixture "$db"
  sqlite3 "$db" "CREATE TABLE session2(id TEXT PRIMARY KEY, time_updated INTEGER); INSERT INTO session2 SELECT id, time_updated FROM session; DROP TABLE session; ALTER TABLE session2 RENAME TO session;"
  check_prune_refused "$db" "session has no 'parent_id' column"

  # Control: the full-schema fixture prunes exactly as before (2 deleted,
  # newest kept, verification passes).
  db="$fx/full.db"; mk_fixture "$db"
  out="$(bash "$PRUNE" --db "$db" --protect-days 0 --apply -y --no-backup 2>&1)"; rc=$?
  if [ "$rc" -ne 0 ]; then
    fail "prune on full-schema fixture exited $rc: $out"
  elif [ "$(event_count "$db")" != "1" ]; then
    fail "prune on full-schema fixture left $(event_count "$db") event rows, expected 1"
  else
    ok "prune full-schema fixture deletes only the 2 superseded snapshots"
  fi

  # Vacuum: missing message table refuses up front; no compacted copy.
  db="$fx/vac-missing-message.db"; mk_fixture "$db"; sqlite3 "$db" "DROP TABLE message;"
  out="$(bash "$VAC" --db "$db" -y 2>&1)"; rc=$?
  if [ "$rc" -eq 0 ]; then
    fail "vacuum on $db: exited 0, expected preflight refusal"
  elif ! printf '%s' "$out" | grep -q -- "message table missing"; then
    fail "vacuum on $db: refusal does not name the missing table: $out"
  elif [ -e "$db.compact" ]; then
    fail "vacuum on $db: compacted copy was created despite refusal"
  else
    ok "vacuum refuses missing-message DB up front, no compacted copy"
  fi
fi

if [ "$failures" -ne 0 ]; then
  echo "$failures failure(s)" >&2
  exit 1
fi
echo "all argument-parser checks passed"
