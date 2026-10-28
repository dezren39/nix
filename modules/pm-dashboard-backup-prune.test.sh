#!/usr/bin/env bash
# Tests for modules/pm-dashboard-backup.sh's retention prune (#982):
#   The script builds and verifies the local archive, then ran its
#   30-day retention prune BEFORE the offsite uploads — and the remote
#   prune ran after each upload with the same cutoff. A backfill /
#   re-run for a date older than the cutoff therefore deleted the
#   just-created, verified archive in the same run: the R2 upload
#   failed on a missing source (or the remote prune deleted the copy
#   it had just uploaded), and no copy existed anywhere.
# This drives the REAL script with stubbed bws / journalctl / rclone
# against a scratch HOME and a fake remote tree, and pins:
#   * an old-date (backfill) run exits 0 and its archive still exists
#     locally and on both remotes after the run;
#   * genuinely old day dirs for OTHER dates are still pruned,
#     locally and remotely, in the same run;
#   * a current-date run is unchanged (archive built, uploaded to
#     both remotes, sha256-verified by the script itself), and the
#     earlier backfill date — no longer the run date — is then
#     pruned by that later run.
#   * a failed remote prune listing (#1021: stub rclone lsf exits 1)
#     fails the run — non-zero exit, a FAILED line naming the remote
#     and the listing step, the stub's stderr in the log, no full OK
#     line, and the journal cursor unchanged — while a genuinely
#     empty successful listing (lsf exits 0, no output) still
#     completes successfully.
# (Named -prune.test.sh because the #981 journal-cursor suite for the
# same script is modules/pm-dashboard-backup.test.sh.)
# Stubs use #!/bin/sh: the nix build sandbox has no /usr/bin/env.
set -uo pipefail

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$SELF_DIR/pm-dashboard-backup.sh"

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

failures=0
fail() {
  echo "FAIL: $1" >&2
  failures=$((failures + 1))
}
check() { # check <description> <command...>
  local desc="$1"; shift
  if "$@" >/dev/null 2>&1; then
    echo "ok: $desc"
  else
    fail "$desc"
  fi
}

HOME_DIR="$TMP_ROOT/home"
BIN="$TMP_ROOT/bin"
REMOTE="$TMP_ROOT/remote"
mkdir -p "$HOME_DIR/.local/state/pm-aggregator" "$HOME_DIR/atlas/meta-quota-export" "$BIN" "$REMOTE"

# --- fixture data set -------------------------------------------------------
for f in actions.json comments.json fleet-feed.json quota-current.json \
         registry.json quota-gatherer-state.json quota-gatherer-heartbeat.json; do
  echo "{\"fixture\": \"$f\"}" > "$HOME_DIR/.local/state/pm-aggregator/$f"
done
echo '{"fixture": "latest"}' > "$HOME_DIR/atlas/meta-quota-export/latest.json"
echo '{"fixture": "history"}' > "$HOME_DIR/atlas/meta-quota-export/history.jsonl"

# --- stubs ------------------------------------------------------------------
cat > "$BIN/bws" <<'EOF'
#!/bin/sh
# bws stub: only `secret list`, with fake credential values (names only matter)
if [ "$1" = "secret" ] && [ "$2" = "list" ]; then
  cat <<'JSON'
[{"key":"cloudflare.r2-backups-access-key-id","value":"test-access-key-id"},
 {"key":"cloudflare.r2-backups-secret-access-key","value":"test-secret-access-key"},
 {"key":"cloudflare.r2-endpoint","value":"https://r2.example.invalid"}]
JSON
  exit 0
fi
echo "bws stub: unexpected args: $*" >&2
exit 1
EOF

cat > "$BIN/journalctl" <<'EOF'
#!/bin/sh
echo "fixture journal line"
exit 0
EOF

cat > "$BIN/rclone" <<'EOF'
#!/bin/sh
# rclone stub backed by $FAKE_REMOTE/<remote-name>/<path>
set -u
map_path() { # $1 = remote:path -> $FAKE_REMOTE/<remote>/<path>
  remote="${1%%:*}"
  rest="${1#*:}"
  printf '%s/%s/%s' "$FAKE_REMOTE" "$remote" "$rest"
}
cmd="$1"; shift
case "$cmd" in
  copyto)
    dest="$(map_path "$2")"
    mkdir -p "$(dirname "$dest")"
    cp "$1" "$dest"
    ;;
  cat)
    cat "$(map_path "$1")"
    ;;
  lsf)
    # last arg is the remote dir; --dirs-only lists subdir names with /
    if [ "${RCLONE_LSF_FAIL:-}" = "1" ]; then echo "rclone stub: forced lsf failure" >&2; exit 1; fi
    if [ "${RCLONE_LSF_EMPTY:-}" = "1" ]; then exit 0; fi
    for last; do :; done
    dir="$(map_path "$last")"
    [ -d "$dir" ] || exit 0
    for d in "$dir"/*/; do
      [ -d "$d" ] && basename "$d" | sed 's|$|/|'
    done
    ;;
  purge)
    rm -rf "$(map_path "$1")"
    ;;
  *)
    echo "rclone stub: unexpected command: $cmd" >&2
    exit 1
    ;;
esac
EOF
chmod +x "$BIN/bws" "$BIN/journalctl" "$BIN/rclone"

ROOT="$HOME_DIR/backups/dashboard"
OLD="2020-01-01"      # the backfill date (older than the 30-day cutoff)
OTHER_OLD="2020-01-02" # a genuinely old day that is never a run date here
TODAY="$(date +%F)"

# A genuinely old day dir, present locally and on both remotes before any run.
mkdir -p "$ROOT/$OTHER_OLD"
echo old > "$ROOT/$OTHER_OLD/dashboard-$OTHER_OLD.tar.zst"
mkdir -p "$REMOTE/r2bak/supabase-backups/dashboard/$OTHER_OLD" \
         "$REMOTE/drive/Cloud/cold-store/db-backups/pm-aggregator/$OTHER_OLD"
echo old > "$REMOTE/r2bak/supabase-backups/dashboard/$OTHER_OLD/dashboard-$OTHER_OLD.tar.zst"
echo old > "$REMOTE/drive/Cloud/cold-store/db-backups/pm-aggregator/$OTHER_OLD/dashboard-$OTHER_OLD.tar.zst"

run_backup() { # run_backup <date> — sets RC; log in $TMP_ROOT/run-<date>.log
  HOME="$HOME_DIR" PATH="$BIN:$PATH" FAKE_REMOTE="$REMOTE" \
    RCLONE_LSF_FAIL="${LSF_FAIL:-}" RCLONE_LSF_EMPTY="${LSF_EMPTY:-}" \
    BWS_ACCESS_TOKEN="test-token" PM_DB_DUMP_SCRIPT="$TMP_ROOT/no-dump-script" \
    bash "$SCRIPT" "$1" > "$TMP_ROOT/run-$1.log" 2>&1
  RC=$?
}

# --- scenario 1: backfill for an old date -----------------------------------
run_backup "$OLD"
if [ "$RC" -eq 0 ]; then
  echo "ok: old-date run exits 0"
else
  fail "old-date run exits 0 — got $RC; log: $(cat "$TMP_ROOT/run-$OLD.log")"
fi
check "old-date archive still exists locally after its own run" \
  test -f "$ROOT/$OLD/dashboard-$OLD.tar.zst"
check "old-date archive exists on the R2 remote after its own run" \
  test -f "$REMOTE/r2bak/supabase-backups/dashboard/$OLD/dashboard-$OLD.tar.zst"
check "old-date archive exists on the Drive remote after its own run" \
  test -f "$REMOTE/drive/Cloud/cold-store/db-backups/pm-aggregator/$OLD/dashboard-$OLD.tar.zst"
check "genuinely old other day pruned locally" \
  test ! -e "$ROOT/$OTHER_OLD"
check "genuinely old other day pruned on R2" \
  test ! -e "$REMOTE/r2bak/supabase-backups/dashboard/$OTHER_OLD"
check "genuinely old other day pruned on Drive" \
  test ! -e "$REMOTE/drive/Cloud/cold-store/db-backups/pm-aggregator/$OTHER_OLD"

# --- scenario 2: the daily current-date run is unchanged --------------------
run_backup "$TODAY"
if [ "$RC" -eq 0 ]; then
  echo "ok: current-date run exits 0"
else
  fail "current-date run exits 0 — got $RC; log: $(cat "$TMP_ROOT/run-$TODAY.log")"
fi
check "current-date archive exists locally" \
  test -f "$ROOT/$TODAY/dashboard-$TODAY.tar.zst"
check "current-date archive exists on the R2 remote" \
  test -f "$REMOTE/r2bak/supabase-backups/dashboard/$TODAY/dashboard-$TODAY.tar.zst"
check "current-date archive exists on the Drive remote" \
  test -f "$REMOTE/drive/Cloud/cold-store/db-backups/pm-aggregator/$TODAY/dashboard-$TODAY.tar.zst"
check "backfill date is pruned by a later run once it is not the run date" \
  test ! -e "$ROOT/$OLD"

# --- scenario 3: a failed prune listing fails the run (#1021) ---------------
PRESET=1000000000
echo "$PRESET" > "$ROOT/.last-run-epoch"
LSF_FAIL=1
run_backup "$TODAY"
LSF_FAIL=""
if [ "$RC" -ne 0 ]; then
  echo "ok: lsf-failure run exits non-zero ($RC)"
else
  fail "lsf-failure run exits non-zero — got 0; log: $(cat "$TMP_ROOT/run-$TODAY.log")"
fi
check "lsf-failure FAILED line names the remote and the listing step" \
  grep -q "pm-dashboard-backup: FAILED prune listing failed on R2" "$TMP_ROOT/run-$TODAY.log"
check "lsf-failure surfaces rclone's stderr in the log" \
  grep -q "rclone stub: forced lsf failure" "$TMP_ROOT/run-$TODAY.log"
if grep -q "OK dashboard $TODAY backed up to local + R2 + Drive" "$TMP_ROOT/run-$TODAY.log"; then
  fail "lsf-failure run printed the full OK line"
else
  echo "ok: lsf-failure run printed no full OK line"
fi
if [ "$(cat "$ROOT/.last-run-epoch")" = "$PRESET" ]; then
  echo "ok: lsf-failure run left the journal cursor unchanged"
else
  fail "lsf-failure run advanced the journal cursor — was $PRESET, now $(cat "$ROOT/.last-run-epoch")"
fi

# --- scenario 4: a genuinely empty successful listing still succeeds --------
# (lsf exits 0 with no output, the fresh-remote shape: nothing to prune
# is a success, not a failure — the distinction #1021 draws.)
LSF_EMPTY=1
run_backup "$TODAY"
LSF_EMPTY=""
if [ "$RC" -eq 0 ]; then
  echo "ok: empty-listing run exits 0"
else
  fail "empty-listing run exits 0 — got $RC; log: $(cat "$TMP_ROOT/run-$TODAY.log")"
fi
check "empty-listing run printed the full OK line" \
  grep -q "OK dashboard $TODAY backed up to local + R2 + Drive" "$TMP_ROOT/run-$TODAY.log"
if [ "$(cat "$ROOT/.last-run-epoch")" != "$PRESET" ]; then
  echo "ok: empty-listing run advanced the journal cursor"
else
  fail "empty-listing run did not advance the journal cursor"
fi

if [ "$failures" -gt 0 ]; then
  echo "$failures failure(s)" >&2
  exit 1
fi
echo "all pm-dashboard-backup prune checks passed"
