#!/usr/bin/env bash
# Fixture test for opencode-db-backup-prune.sh (#659 retention).
#
# Builds a fixture backup tree — timestamp-named dirs plus entries the
# pruner must never touch — and pins: dry-run lists exactly the
# beyond-keep candidates and deletes nothing; --apply deletes exactly
# the listed set; the newest backup is never listed even with
# --keep 1; --max-age-days filters out recent candidates; non-timestamp
# entries survive everything; --keep 0 is rejected.
#
# Run: ./opencode-db-backup-prune.test.sh
# It is run by the flake check `opencode-db-backup-prune-tests`.
set -u

cd "$(dirname "$0")" || exit

failures=0
fail() { echo "FAIL: $1" >&2; failures=$((failures + 1)); }
ok() { echo "ok: $1"; }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
PRUNE=./opencode-db-backup-prune.sh

make_tree() {
  local base="$1"
  mkdir -p "$base"
  local d
  for d in 20260101T000000 20260201T000000 20260301T000000 20260401T000000 20260501T000000; do
    mkdir -p "$base/$d"
    echo fixture > "$base/$d/opencode.db"
  done
  mkdir -p "$base/not-a-backup"
  echo keep > "$base/not-a-backup/file"
  echo keep > "$base/loose-file.txt"
}

listing() { ls "$1" | sort | tr '\n' ' '; }

# --- dry run, --keep 2: lists oldest 3, deletes nothing -------------------
B="$WORK/dry"; make_tree "$B"
out="$(bash "$PRUNE" --dir "$B" --keep 2 2>&1)"; rc=$?
[ "$rc" -eq 0 ] || fail "dry run exited $rc: $out"
n="$(printf '%s' "$out" | grep -c 'would delete:')"
[ "$n" -eq 3 ] || fail "dry run listed $n candidates, expected 3: $out"
for d in 20260101T000000 20260201T000000 20260301T000000; do
  printf '%s' "$out" | grep -q "would delete: $B/$d\$" || fail "dry run did not list $d: $out"
done
printf '%s' "$out" | grep -q "20260501T000000" && fail "dry run listed the newest backup"
[ "$(listing "$B")" = "20260101T000000 20260201T000000 20260301T000000 20260401T000000 20260501T000000 loose-file.txt not-a-backup " ] \
  || fail "dry run changed the tree: $(listing "$B")"
ok "dry run lists exactly the 3 beyond-keep backups and deletes nothing"

# --- apply, --keep 2: deletes exactly the listed set ----------------------
out="$(bash "$PRUNE" --dir "$B" --keep 2 --apply 2>&1)"; rc=$?
[ "$rc" -eq 0 ] || fail "apply exited $rc: $out"
[ "$(listing "$B")" = "20260401T000000 20260501T000000 loose-file.txt not-a-backup " ] \
  || fail "apply left wrong tree: $(listing "$B")"
ok "apply deletes exactly the listed set; non-timestamp entries survive"

# --- --keep 1: newest still never deleted ---------------------------------
B="$WORK/one"; make_tree "$B"
bash "$PRUNE" --dir "$B" --keep 1 --apply >/dev/null 2>&1
[ "$(listing "$B")" = "20260501T000000 loose-file.txt not-a-backup " ] \
  || fail "--keep 1 left wrong tree: $(listing "$B")"
ok "--keep 1 keeps only the newest backup"

# --- --max-age-days: recent candidates are filtered out -------------------
B="$WORK/age"; make_tree "$B"
recent="$(date +%Y%m%dT%H%M%S)"
mkdir -p "$B/$recent"
# keep 1 would list all but the newest (the recent dir); with
# --max-age-days 30 the 2026 fixtures (all >30d old at any run date in
# 2026+) are listed, and nothing newer than the cutoff can appear —
# pin the invariant via a huge max age instead: nothing is old enough.
out="$(bash "$PRUNE" --dir "$B" --keep 1 --max-age-days 36500 2>&1)"
printf '%s' "$out" | grep -q 'would delete:' \
  && fail "--max-age-days 36500 listed candidates: $out"
ok "--max-age-days filters out candidates not old enough"

# --- guards ---------------------------------------------------------------
out="$(bash "$PRUNE" --dir "$WORK/none" --keep 0 2>&1)"; rc=$?
[ "$rc" -ne 0 ] && printf '%s' "$out" | grep -q -- "--keep" \
  || fail "--keep 0 not rejected naming --keep: rc=$rc out=$out"
ok "--keep 0 rejected (newest backup can never be deleted)"

out="$(bash "$PRUNE" --dir "$WORK/does-not-exist" 2>&1)"; rc=$?
[ "$rc" -eq 0 ] || fail "missing dir should be a no-op success, got rc=$rc: $out"
ok "missing backup dir is a no-op success"

out="$(bash "$PRUNE" --keep --apply 2>&1)"; rc=$?
[ "$rc" -ne 0 ] && printf '%s' "$out" | grep -q -- "--keep" \
  || fail "flag-shaped --keep value not rejected naming --keep: rc=$rc out=$out"
ok "flag-shaped values rejected naming the option (#930 pattern)"

if [ "$failures" -gt 0 ]; then
  echo "opencode-db-backup-prune.test: $failures failure(s)" >&2
  exit 1
fi
echo "opencode-db-backup-prune.test: all checks passed"
