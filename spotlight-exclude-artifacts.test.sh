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

[ "$failures" -eq 0 ] || { echo "$failures failure(s)" >&2; exit 1; }
echo "all spotlight-exclude-artifacts flag-shaped-value checks passed"
