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
out="$(bash "$PRUNE" --dir "$B" --keep 1 --max-age-days 36500 2>&1)"; rc=$?
[ "$rc" -eq 0 ] || fail "--max-age-days 36500 exited $rc: $out"
printf '%s' "$out" | grep -q 'would delete:' \
  && fail "--max-age-days 36500 listed candidates: $out"
printf '%s' "$out" | grep -q "dry run — 0 of 6 backup(s)" \
  || fail "--max-age-days 36500 missing 0-of-6 summary: $out"
ok "--max-age-days filters out candidates not old enough"

# --- --max-age-days: old candidates ARE listed (positive case) ------------
# Regression test for #1065: the case above can be satisfied by a run that
# dies on the first candidate, so pin a positive listing, exit code 0, and
# the summary line. The 2026 fixtures are all >30d old at any run date
# after 2026-06, so --keep 1 --max-age-days 30 lists the oldest 4.
B="$WORK/age-positive"; make_tree "$B"
out="$(bash "$PRUNE" --dir "$B" --keep 1 --max-age-days 30 2>&1)"; rc=$?
[ "$rc" -eq 0 ] || fail "--max-age-days 30 exited $rc: $out"
n="$(printf '%s' "$out" | grep -c 'would delete:')"
[ "$n" -eq 4 ] || fail "--max-age-days 30 listed $n candidates, expected 4: $out"
for d in 20260101T000000 20260201T000000 20260301T000000 20260401T000000; do
  printf '%s' "$out" | grep -q "would delete: $B/$d\$" || fail "--max-age-days 30 did not list $d: $out"
done
printf '%s' "$out" | grep -q "dry run — 4 of 5 backup(s)" \
  || fail "--max-age-days 30 missing 4-of-5 summary: $out"
ok "--max-age-days lists old candidates, exits 0, and reports the summary"

# --- --max-age-days under a BSD-date shim: no GNU date -d ------------------
# macOS date has no -d (it parses with -j -f); emulate it on any host by
# rejecting -d and translating -j -f to the real (GNU) date. The prune
# must still list the old candidates and exit 0 (#1065).
SHIM="$WORK/bsd-shim"; mkdir -p "$SHIM"
REAL_DATE="$(command -v date)"
cat > "$SHIM/date" <<SHIM_EOF
#!/bin/sh
if [ "\$1" = "-j" ]; then
  # date -j -f FORMAT INPUT +%s
  input="\$4"
  exec "$REAL_DATE" -d "\$input" +%s
fi
for a in "\$@"; do
  [ "\$a" = "-d" ] && { echo "date: illegal option -- d" >&2; exit 1; }
done
exec "$REAL_DATE" "\$@"
SHIM_EOF
chmod +x "$SHIM/date"
B="$WORK/age-bsd"; make_tree "$B"
out="$(PATH="$SHIM:$PATH" bash "$PRUNE" --dir "$B" --keep 1 --max-age-days 30 2>&1)"; rc=$?
[ "$rc" -eq 0 ] || fail "--max-age-days 30 under BSD date exited $rc: $out"
n="$(printf '%s' "$out" | grep -c 'would delete:')"
[ "$n" -eq 4 ] || fail "--max-age-days 30 under BSD date listed $n candidates, expected 4: $out"
printf '%s' "$out" | grep -q "dry run — 4 of 5 backup(s)" \
  || fail "--max-age-days 30 under BSD date missing 4-of-5 summary: $out"
ok "--max-age-days works with BSD date (no GNU date -d)"

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

# --- numeric validation: leading zeros and range backstops (#1087) -------
# --keep 08 with more backups than the keep count reaches the arithmetic
# path that used to crash on the octal-invalid literal: 10 dirs, keep 08
# (decimal 8) lists exactly the oldest 2, exits 0, no raw bash errors.
B="$WORK/zeros"; mkdir -p "$B"
for d in 20260101T000000 20260102T000000 20260103T000000 20260104T000000 20260105T000000 20260106T000000 20260107T000000 20260108T000000 20260109T000000 20260110T000000; do
  mkdir -p "$B/$d"; echo fixture > "$B/$d/opencode.db"
done
out="$(bash "$PRUNE" --dir "$B" --keep 08 2>&1)"; rc=$?
[ "$rc" -eq 0 ] || fail "--keep 08 exited $rc: $out"
n="$(printf '%s' "$out" | grep -c 'would delete:')"
[ "$n" -eq 2 ] || fail "--keep 08 listed $n candidates, expected 2: $out"
printf '%s' "$out" | grep -q 'value too great for base' && fail "--keep 08 leaked a raw octal error: $out"
printf '%s' "$out" | grep -q 'kept newest 8' || fail "--keep 08 summary does not say kept newest 8: $out"
ok "--keep 08 behaves as decimal 8 on the arithmetic path"

# --keep 010 is decimal 10, not octal 8: with 10 dirs nothing is pruned.
out="$(bash "$PRUNE" --dir "$B" --keep 010 2>&1)"; rc=$?
[ "$rc" -eq 0 ] || fail "--keep 010 exited $rc: $out"
printf '%s' "$out" | grep -q 'nothing to prune' || fail "--keep 010 did not behave as decimal 10: $out"
ok "--keep 010 behaves as decimal 10, not octal 8"

# Oversized --keep: one clear parse-time rejection naming the option and
# the value, exit 2, no raw integer-expression leak, no misleading reason.
out="$(bash "$PRUNE" --dir "$B" --keep 99999999999999999999999 2>&1)"; rc=$?
[ "$rc" -eq 2 ] || fail "oversized --keep exited $rc, expected 2: $out"
printf '%s' "$out" | grep -q -- '--keep' || fail "oversized --keep rejection does not name --keep: $out"
printf '%s' "$out" | grep -q '99999999999999999999999' || fail "oversized --keep rejection does not name the value: $out"
printf '%s' "$out" | grep -q 'integer expression expected' && fail "oversized --keep leaked a raw test error: $out"
printf '%s' "$out" | grep -q 'at least 1' && fail "oversized --keep died with the misleading at-least-1 reason: $out"
ok "oversized --keep rejected at parse time naming option and value"

# Oversized --max-age-days: same parse-time rejection; it must never reach
# the per-candidate test, which used to error per candidate and exit 0
# with age pruning silently disabled.
out="$(bash "$PRUNE" --dir "$B" --keep 2 --max-age-days 999999999999999999999 2>&1)"; rc=$?
[ "$rc" -eq 2 ] || fail "oversized --max-age-days exited $rc, expected 2: $out"
printf '%s' "$out" | grep -q -- '--max-age-days' || fail "oversized --max-age-days rejection does not name --max-age-days: $out"
printf '%s' "$out" | grep -q 'integer expression expected' && fail "oversized --max-age-days leaked a raw test error: $out"
ok "oversized --max-age-days rejected at parse time (no silent exit 0)"

if [ "$failures" -gt 0 ]; then
  echo "opencode-db-backup-prune.test: $failures failure(s)" >&2
  exit 1
fi
echo "opencode-db-backup-prune.test: all checks passed"
