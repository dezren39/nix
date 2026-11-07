#!/usr/bin/env bash
# Regression test for spotlight-exclude-artifacts's flag-shaped-value
# guards (#951) and second-target / repeat / conflict guards (#1119).
#
# PR #939 (fix for #930) made spotlight-exclude-artifacts's value-taking
# options reject a flag-shaped next token — `--path --dry-run` used to
# swallow --dry-run as the root and run its real marking pass — but
# added no test for spotlight-exclude-artifacts, so the guards could
# regress silently.
#
# The contract pinned here, for --path and --depth:
#   <option> --dry-run  -> exit 2, "<option> requires a value" on
#                          stderr, and no marker files written
#   valid values        -> accepted: the --dry-run control run below
#                          parses both options with real values,
#                          discovers the fixture repo, and exits 0
#                          still writing no markers (dry run)
#
# This drives the REAL spotlight-exclude-artifacts (no pasted copies)
# against temp dirs. The fixture repo is a plain directory with a .git
# directory, which is all discovery looks for. The script shells out to
# the sibling git-discover-repos, so the suite must run from a tree
# that has both scripts and lib/common.sh (the repo root, or the flake
# check's copies).
set -uo pipefail

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$SELF_DIR/spotlight-exclude-artifacts"
# Shared conformance probe matrix helpers (#1146, design #1145 phase 1).
# shellcheck source=lib/arg-probe.sh
. "$SELF_DIR/lib/arg-probe.sh"

MARKER=".metadata_never_index"

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT
export HOME="$TMP_ROOT/home"
mkdir -p "$HOME"

failures=0
fail() {
  echo "FAIL: $1" >&2
  failures=$((failures + 1))
}

VALID="$TMP_ROOT/valid"
mkdir -p "$VALID/repo1/.git"

run() { # run <args...> — sets OUT, ERR, STATUS
  OUT="$(bash "$SCRIPT" "$@" 2>"$TMP_ROOT/err")"; STATUS=$?
  ERR="$(cat "$TMP_ROOT/err")"
}

markers() { find "$TMP_ROOT" -name "$MARKER" | sort; }

# ─── Flag-shaped values are rejected at parse time ─────────────────────
for opt in --path --depth; do
  run "$opt" --dry-run
  [ "$STATUS" -eq 2 ] || fail "$opt --dry-run exited $STATUS (want 2)"
  case "$ERR" in
    *"spotlight-exclude-artifacts: $opt requires a value"*) echo "ok: $opt --dry-run rejected, option named" ;;
    *) fail "$opt --dry-run did not name the option on stderr (got: $ERR)" ;;
  esac
done

# ─── Missing values are rejected at parse time (#970) ──────────
# PR #921 made the value-taking options reject a missing value (option
# last, or an empty string) with exit 2, but no suite pinned that branch:
# a refactor dropping the -n check would pass the flag-shaped runs above
# while a trailing option silently took an empty value. Each option must
# exit 2 naming itself for both shapes.
for opt in --path --depth; do
  run "$opt"
  [ "$STATUS" -eq 2 ] || fail "$opt (trailing, no value) exited $STATUS (want 2)"
  case "$ERR" in
    *"spotlight-exclude-artifacts: $opt requires a value"*) echo "ok: $opt trailing rejected, option named" ;;
    *) fail "$opt (trailing, no value) did not name the option on stderr (got: $ERR)" ;;
  esac
  run "$opt" ""
  [ "$STATUS" -eq 2 ] || fail "$opt (empty value) exited $STATUS (want 2)"
  case "$ERR" in
    *"spotlight-exclude-artifacts: $opt requires a value"*) echo "ok: $opt empty value rejected, option named" ;;
    *) fail "$opt (empty value) did not name the option on stderr (got: $ERR)" ;;
  esac
done


# ─── Second target is rejected at parse time (#1119) ──────────────────
# --path used to assign ROOT unconditionally, so a second target in
# --path or mixed form silently replaced the first and the run exited 0
# having never walked it (the #1111 clean.sh shape). All three forms
# must exit 2 naming the offending argument.
SECOND_A="$TMP_ROOT/second-a"
SECOND_B="$TMP_ROOT/second-b"
mkdir -p "$SECOND_A" "$SECOND_B"
run --no-system --dry-run --path "$SECOND_A" --path "$SECOND_B"
[ "$STATUS" -eq 2 ] || fail "--path A --path B exited $STATUS (want 2)"
case "$ERR" in
  *"unexpected argument: $SECOND_B"*) echo "ok: --path after --path rejected, second target named" ;;
  *) fail "--path after --path did not name the second target (got: $ERR)" ;;
esac
run --no-system --dry-run "$SECOND_A" --path "$SECOND_B"
[ "$STATUS" -eq 2 ] || fail "positional then --path exited $STATUS (want 2)"
case "$ERR" in
  *"unexpected argument: $SECOND_B"*) echo "ok: --path after positional rejected, second target named" ;;
  *) fail "--path after positional did not name the second target (got: $ERR)" ;;
esac
run --no-system --dry-run --path "$SECOND_B" "$SECOND_A"
[ "$STATUS" -eq 2 ] || fail "--path then positional exited $STATUS (want 2)"
case "$ERR" in
  *"unexpected argument: $SECOND_A"*) echo "ok: positional after --path rejected, second target named" ;;
  *) fail "positional after --path did not name the second target (got: $ERR)" ;;
esac

# ─── Repeated --depth is rejected at parse time (#1119, #1103 style) ──
run --no-system --dry-run --path "$VALID" --depth 1 --depth 3
[ "$STATUS" -eq 2 ] || fail "repeated --depth exited $STATUS (want 2)"
case "$ERR" in
  *"--depth may be given only once"*) echo "ok: repeated --depth rejected, only-once rule named" ;;
  *) fail "repeated --depth did not name the only-once rule (got: $ERR)" ;;
esac

# ─── --follow with --no-follow is a conflict (#1119) ──────────────────
run --no-system --dry-run --path "$VALID" --follow --no-follow
[ "$STATUS" -eq 2 ] || fail "--follow --no-follow exited $STATUS (want 2)"
case "$ERR" in
  *"--no-follow conflicts with --follow"*) echo "ok: --follow then --no-follow rejected as a conflict" ;;
  *) fail "--follow --no-follow did not name the conflict (got: $ERR)" ;;
esac
run --no-system --dry-run --path "$VALID" --no-follow --follow
[ "$STATUS" -eq 2 ] || fail "--no-follow --follow exited $STATUS (want 2)"
case "$ERR" in
  *"--follow conflicts with --no-follow"*) echo "ok: --no-follow then --follow rejected as a conflict" ;;
  *) fail "--no-follow --follow did not name the conflict (got: $ERR)" ;;
esac

# --- --system with --no-system is a conflict (#1135) ---
# git-maintain-repos rejected this pair in #1124, but spotlight's two
# flags both wrote the single SCOPE_SET, so the last one silently won.
for pair in "--system --no-system" "--no-system --system"; do
  # shellcheck disable=SC2086
  run --dry-run --path "$VALID" $pair
  [ "$STATUS" -eq 2 ] || fail "scope pair '$pair' exited $STATUS (want 2)"
  case "$ERR" in
    *"spotlight-exclude-artifacts: --system and --no-system may not be combined"*) echo "ok: scope pair '$pair' rejected as a conflict" ;;
    *) fail "scope pair '$pair' was not named as a conflict on stderr (got: $ERR)" ;;
  esac
done
run --dry-run --path "$VALID" --system
[ "$STATUS" -eq 0 ] || fail "single --system exited $STATUS (want 0)"
echo "ok: single --system still accepted"
run --dry-run --path "$VALID" --no-system
[ "$STATUS" -eq 0 ] || fail "single --no-system exited $STATUS (want 0)"
echo "ok: single --no-system still accepted"

# ─── No side effects from the rejected runs ────────────────────────────
if [ -z "$(markers)" ]; then
  echo "ok: rejected runs wrote no markers"
else
  fail "rejected runs wrote marker files: $(markers)"
fi

# ─── Valid-value control (dry run: discovers, marks nothing) ──────────
run --no-system --path "$VALID" --depth 2 --dry-run
[ "$STATUS" -eq 0 ] || fail "valid-value control exited $STATUS (stderr: $ERR)"
case "$OUT" in
  *"discovering repos"*) echo "ok: valid values parse and the control run exits 0" ;;
  *) fail "valid-value control output changed (got: $OUT)" ;;
esac
case "$OUT" in
  *"would mark: $VALID/repo1/.git"*) echo "ok: control discovered the fixture repo and would mark its .git" ;;
  *) fail "control did not discover the fixture repo (got: $OUT)" ;;
esac
if [ -z "$(markers)" ]; then
  echo "ok: dry-run control wrote no markers"
else
  fail "dry-run control wrote marker files: $(markers)"
fi

# ─── --no-recursive --follow is rejected at parse time (#1268) ─────
# git-discover-repos rejects this pair at exit 2 (spotlight forwards
# --no-recursive as --depth 1, the same combination in discover's
# vocabulary), but spotlight used to read discovery through a process
# substitution with stderr discarded, so the failure looked like an
# empty tree: "0 marked" at exit 0. Both orders, and the explicit
# --depth 1 spelling, must now exit 2 naming the pair, writing nothing.
for pair in "--no-recursive --follow" "--follow --no-recursive" "--depth 1 --follow"; do
  # shellcheck disable=SC2086
  run --no-system --dry-run --path "$VALID" $pair
  [ "$STATUS" -eq 2 ] || fail "no-recursive/follow pair '$pair' exited $STATUS (want 2)"
  case "$ERR" in
    *"spotlight-exclude-artifacts: --no-recursive conflicts with --follow"*) echo "ok: pair '$pair' rejected as a conflict" ;;
    *) fail "pair '$pair' was not named as a conflict on stderr (got: $ERR)" ;;
  esac
done
[ -z "$(markers)" ] && echo "ok: rejected conflict runs wrote no markers" || fail "rejected conflict runs wrote markers"
# Control: --no-recursive alone still discovers and would mark the
# repo (depth 1 matches the repo at the path itself).
run --no-system --dry-run --path "$VALID/repo1" --no-recursive
[ "$STATUS" -eq 0 ] || fail "--no-recursive control exited $STATUS (want 0)"
case "$OUT" in
  *"would mark: $VALID/repo1/.git"*) echo "ok: --no-recursive control would mark the fixture repo" ;;
  *) fail "--no-recursive control did not report the fixture repo (got: $OUT)" ;;
esac

# ─── Shared conformance probe matrix (#1146, design #1145) ─────────
# The matrix rows for spotlight-exclude-artifacts, driven through
# lib/arg-probe.sh (the same helper every discovery suite uses):
# unknown, missing/flag-shaped (also pinned per-option above), repeat,
# both conflict orders, extra positional, and bad numbers -- abc, 0,
# oversized -- for --depth. 08 is probed as accepted decimal 8
# (rule 6, #881).
probe_rejected "unknown option: --bogus" --bogus
probe_rejected "spotlight-exclude-artifacts: --depth requires a value" --depth
probe_rejected "spotlight-exclude-artifacts: --depth requires a value" --depth --dry-run
probe_rejected "spotlight-exclude-artifacts: --depth may be given only once" --no-system --dry-run --path "$VALID" --depth 1 --depth 3
probe_rejected "--no-follow conflicts with --follow" --no-system --dry-run --path "$VALID" --follow --no-follow
probe_rejected "--follow conflicts with --no-follow" --no-system --dry-run --path "$VALID" --no-follow --follow
probe_rejected "spotlight-exclude-artifacts: --system and --no-system may not be combined" --dry-run --system --no-system
probe_rejected "spotlight-exclude-artifacts: --system and --no-system may not be combined" --dry-run --no-system --system
probe_rejected "unexpected argument: $SECOND_A" --no-system --dry-run --path "$SECOND_B" "$SECOND_A"
probe_rejected "spotlight-exclude-artifacts: empty positional argument (target must not be empty)" --no-system --dry-run ""
for bad in abc 0 2147483648 99999999999999999999999999; do
  probe_rejected "spotlight-exclude-artifacts: --depth must be a positive integer" --no-system --dry-run --path "$VALID" --depth "$bad"
done
probe_accepted --no-system --path "$VALID" --depth 08 --dry-run


# ─── Failed marker writes exit non-zero (#1333) ─────────────────────
# mark() counted a failed touch in the summary ("N failed") but the
# exit path only honoured DISCOVERY_FAILED, so the run exited 0.
# Fixture: a dangling marker symlink -- [ -e ] is false, touch follows
# the symlink and fails on a writable directory, so the needroot case
# does not apply and the write increments failed.
# Walk half: repo1's .git carries the dangling marker; repo2 is clean
# and must still be marked (no early abort).
WALKFAIL="$TMP_ROOT/walkfail"
mkdir -p "$WALKFAIL/repo1/.git" "$WALKFAIL/repo2/.git"
ln -s "/nonexistent-spotlight-1333/marker" "$WALKFAIL/repo1/.git/$MARKER"
run --no-system --path "$WALKFAIL" --depth 2
[ "$STATUS" -eq 1 ] || fail "walk failing-write run exited $STATUS (want 1)"
case "$ERR" in
  *"cannot write $WALKFAIL/repo1/.git/$MARKER"*) echo "ok: walk failing write named on stderr" ;;
  *) fail "walk failing write not named on stderr (got: $ERR)" ;;
esac
case "$OUT" in
  *"1 failed"*) echo "ok: walk failing run summary reports 1 failed" ;;
  *) fail "walk failing run summary did not report 1 failed (got: $OUT)" ;;
esac
[ -e "$WALKFAIL/repo2/.git/$MARKER" ] && echo "ok: walk failing run still marked the clean repo" || fail "walk failing run did not mark the clean repo (early abort?)"
# Clean control: remove the dangling marker and the same walk exits 0.
rm "$WALKFAIL/repo1/.git/$MARKER"
run --no-system --path "$WALKFAIL" --depth 2
[ "$STATUS" -eq 0 ] || fail "walk clean control exited $STATUS (want 0, stderr: $ERR)"
case "$OUT" in
  *"0 failed"*) echo "ok: walk clean control exits 0 with 0 failed" ;;
  *) fail "walk clean control summary changed (got: $OUT)" ;;
esac

# System half: same dangling-marker fixture in $HOME/.cache, with a
# clean $HOME/.bun after it in STATIC_PATHS (must still be marked).
# Assertions on the count are loose here: real system paths outside
# the fixture HOME (/nix, /Library, ...) vary by machine.
SYS_HOME="$TMP_ROOT/syshome"
mkdir -p "$SYS_HOME/.cache" "$SYS_HOME/.bun"
ln -s "/nonexistent-spotlight-1333/marker" "$SYS_HOME/.cache/$MARKER"
OLD_HOME="$HOME"
export HOME="$SYS_HOME"
# Isolate the host from the absolute STATIC_PATHS (/Library/..., /nix,
# /var/root/...): changing HOME only redirects the home-relative
# entries. Stub touch so writes under TMP_ROOT are real (the fixture
# .cache failure and .bun success depend on them) and every other
# marker write is a no-op success (counted as marked, writes nothing).
REAL_TOUCH="$(command -v touch)"
mkdir -p "$TMP_ROOT/fakebin"
printf '#!%s\ncase "${1:-}" in\n  "%s"*) exec "%s" "$@" ;;\n  *) exit 0 ;;\nesac\n' "$BASH" "$TMP_ROOT" "$REAL_TOUCH" > "$TMP_ROOT/fakebin/touch"
chmod +x "$TMP_ROOT/fakebin/touch"
OLD_PATH="$PATH"
export PATH="$TMP_ROOT/fakebin:$PATH"
run --system
export PATH="$OLD_PATH"
export HOME="$OLD_HOME"
[ "$STATUS" -eq 1 ] || fail "system failing-write run exited $STATUS (want 1)"
case "$ERR" in
  *"cannot write $SYS_HOME/.cache/$MARKER"*) echo "ok: system failing write named on stderr" ;;
  *) fail "system failing write not named on stderr (got: $ERR)" ;;
esac
case "$OUT" in
  *"0 failed"*) fail "system failing run summary reported 0 failed (got: $OUT)" ;;
  *" failed"*) echo "ok: system failing run summary reports a non-zero failed count" ;;
  *) fail "system failing run printed no summary (got: $OUT)" ;;
esac
[ -e "$SYS_HOME/.bun/$MARKER" ] && echo "ok: system failing run still marked the clean path" || fail "system failing run did not mark the clean .bun path (early abort?)"


# ─── Symlinked markers are never followed (#1586) ─────────────────
# A marker path that is itself a symlink must not be followed: no file
# is created/updated outside the directory being marked, and the
# directory counts as failed -- never as marked or already present.
# Variant 1: dangling symlink whose target parent exists and is
# writable (touch would succeed outside the tree -- the case #1333's
# nonexistent-parent fixture cannot reach). Variant 2: live symlink
# to a real file ([ -e ] follows it, so the old code counted the
# directory as already present and left it unmarked).
LINKFAIL="$TMP_ROOT/linkfail"
OUTSIDE="$TMP_ROOT/outside"
mkdir -p "$LINKFAIL/repo1/.git" "$LINKFAIL/repo2/.git" "$OUTSIDE"
ln -s "$OUTSIDE/dangling-target" "$LINKFAIL/repo1/.git/$MARKER"
run --no-system --path "$LINKFAIL" --depth 2
[ "$STATUS" -eq 1 ] || fail "dangling-symlink run exited $STATUS (want 1)"
case "$ERR" in
  *"cannot write $LINKFAIL/repo1/.git/$MARKER (symlink; not followed)"*) echo "ok: dangling symlink named on stderr" ;;
  *) fail "dangling symlink not named on stderr (got: $ERR)" ;;
esac
case "$OUT" in
  *"1 failed"*) echo "ok: dangling symlink counted as failed" ;;
  *) fail "dangling symlink not counted as failed (got: $OUT)" ;;
esac
[ ! -e "$OUTSIDE/dangling-target" ] && echo "ok: dangling symlink target was not created outside the tree" || fail "dangling symlink target WAS created outside the tree"
[ -L "$LINKFAIL/repo1/.git/$MARKER" ] && echo "ok: dangling marker is still just the symlink" || fail "dangling marker was replaced"
[ -e "$LINKFAIL/repo2/.git/$MARKER" ] && echo "ok: symlink run still marked the clean repo" || fail "symlink run did not mark the clean repo (early abort?)"

LIVEFAIL="$TMP_ROOT/livefail"
mkdir -p "$LIVEFAIL/repo1/.git"
printf 'precious\n' > "$OUTSIDE/live-target"
ln -s "$OUTSIDE/live-target" "$LIVEFAIL/repo1/.git/$MARKER"
run --no-system --path "$LIVEFAIL" --depth 2
[ "$STATUS" -eq 1 ] || fail "live-symlink run exited $STATUS (want 1)"
case "$ERR" in
  *"cannot write $LIVEFAIL/repo1/.git/$MARKER (symlink; not followed)"*) echo "ok: live symlink named on stderr" ;;
  *) fail "live symlink not named on stderr (got: $ERR)" ;;
esac
case "$OUT" in
  *"0 marked, 0 already"*) echo "ok: live symlink counted as neither marked nor already present" ;;
  *) fail "live symlink miscounted (got: $OUT)" ;;
esac
case "$OUT" in
  *"1 failed"*) echo "ok: live symlink counted as failed" ;;
  *) fail "live symlink not counted as failed (got: $OUT)" ;;
esac
[ "$(cat "$OUTSIDE/live-target")" = "precious" ] && echo "ok: live symlink target untouched" || fail "live symlink target was modified"

# spotlight_mark (lib/common.sh) directly: same two variants, oracle is
# return 2 + warning + no outside write (clean.sh's mark_dir delegates
# here, and its own pre-check carries the same -L guard).
(
  # shellcheck source=lib/common.sh
  . "$SELF_DIR/lib/common.sh"
  d1="$TMP_ROOT/markfn-dangling"; mkdir -p "$d1"
  ln -s "$OUTSIDE/fn-dangling-target" "$d1/$MARKER"
  spotlight_mark "$d1" 2>"$TMP_ROOT/markfn.err"; rc=$?
  [ "$rc" -eq 2 ] || { echo "FAIL: spotlight_mark dangling symlink returned $rc (want 2)" >&2; exit 1; }
  grep -q "symlink; not followed" "$TMP_ROOT/markfn.err" || { echo "FAIL: spotlight_mark dangling symlink gave no warning" >&2; exit 1; }
  [ ! -e "$OUTSIDE/fn-dangling-target" ] || { echo "FAIL: spotlight_mark created the dangling target outside" >&2; exit 1; }
  d2="$TMP_ROOT/markfn-live"; mkdir -p "$d2"
  ln -s "$OUTSIDE/live-target" "$d2/$MARKER"
  spotlight_mark "$d2" 2>"$TMP_ROOT/markfn2.err"; rc=$?
  [ "$rc" -eq 2 ] || { echo "FAIL: spotlight_mark live symlink returned $rc (want 2, not 1=already present)" >&2; exit 1; }
  echo "ok: spotlight_mark refuses both symlink variants with return 2"
) || fail "spotlight_mark symlink probe failed (see stderr above)"

[ "$failures" -eq 0 ] || { echo "$failures failure(s)" >&2; exit 1; }
echo "all spotlight-exclude-artifacts flag-shaped-value checks passed"
