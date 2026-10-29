#!/usr/bin/env bash
# Regression test for git-discover-repos's --path exit status (#940).
#
# seed_path() used to print "not a directory: <path>" for a bad --path and
# continue, and nothing recorded the failure, so the script exited 0: a
# caller checking the exit code (clean.sh's `|| warn "discovery failed"`)
# saw a successful empty scan instead of a bad argument, and a typo'd root
# in a mixed run was silently dropped from the scan set.
#
# The contract pinned here (exit 1, matching git-maintain-repos's
# non-directory root):
#   bad --path only        -> non-zero, path named on stderr, no stdout
#   mixed bad + valid      -> non-zero, valid repos still listed
#   a regular file as path -> non-zero (it is not a directory)
#   --count / --dry-run    -> same verdicts; valid input unchanged (exit 0)
#   all-valid --path       -> exit 0, output unchanged
#
# This drives the REAL git-discover-repos (no pasted copies) against temp
# dirs. The fixture repo is a plain directory with a .git directory, which
# is all discovery's classify() looks for — no git binary needed.
set -uo pipefail

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$SELF_DIR/git-discover-repos"

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
# The script canonicalises paths (pwd -P), so expect the canonical form.
VALID_CANON="$(cd "$VALID" && pwd -P)"
BAD="$TMP_ROOT/nonexistent-xyz"
NOTDIR="$TMP_ROOT/a-file"
printf 'x\n' > "$NOTDIR"

run() { # run <args...> — sets OUT, ERR, STATUS
  OUT="$("$SCRIPT" "$@" 2>"$TMP_ROOT/err")"; STATUS=$?
  ERR="$(cat "$TMP_ROOT/err")"
}

# ─── Bad path only ──────────────────────────────────────────────────────────
group_start=$failures
run --path "$BAD"
[ "$STATUS" -ne 0 ] || fail "bad --path exited 0"
case "$ERR" in *"not a directory: $BAD"*) echo "ok: bad path named on stderr" ;; *) fail "bad path not named on stderr (got: $ERR)" ;; esac
[ -z "$OUT" ] || fail "bad --path printed stdout: $OUT"
if [ "$failures" -eq "$group_start" ]; then
  echo "ok: bad --path exits $STATUS with no stdout"
fi

# ─── Mixed bad + valid ──────────────────────────────────────────────────────
run --path "$BAD" --path "$VALID"
[ "$STATUS" -ne 0 ] || fail "mixed bad+valid --path exited 0"
case "$OUT" in *"repo"$'\t'"$VALID_CANON/repo1"*) echo "ok: mixed run still lists the valid repo" ;; *) fail "mixed run did not list the valid repo (got: $OUT)" ;; esac
case "$ERR" in *"not a directory: $BAD"*) : ;; *) fail "mixed run did not name the bad path on stderr (got: $ERR)" ;; esac

# ─── A regular file is not a directory ──────────────────────────────────────
group_start=$failures
run --path "$NOTDIR"
[ "$STATUS" -ne 0 ] || fail "--path naming a regular file exited 0"
if [ "$failures" -eq "$group_start" ]; then
  echo "ok: regular-file --path exits $STATUS"
fi

# ─── All-valid control ──────────────────────────────────────────────────────
run --path "$VALID"
[ "$STATUS" -eq 0 ] || fail "all-valid --path exited $STATUS"
case "$OUT" in *"repo"$'\t'"$VALID_CANON/repo1"*) echo "ok: all-valid run lists the repo and exits 0" ;; *) fail "all-valid run output changed (got: $OUT)" ;; esac

# ─── --count follows the same verdict ───────────────────────────────────────
group_start=$failures
run --path "$VALID" --count
[ "$STATUS" -eq 0 ] || fail "--count on valid input exited $STATUS"
run --path "$BAD" --count
[ "$STATUS" -ne 0 ] || fail "--count on a bad --path exited 0"
if [ "$failures" -eq "$group_start" ]; then
  echo "ok: --count valid exits 0, bad exits non-zero"
fi

# ─── --dry-run follows the same verdict ─────────────────────────────────────
group_start=$failures
run --path "$VALID" --dry-run
[ "$STATUS" -eq 0 ] || fail "--dry-run on valid input exited $STATUS"
run --path "$BAD" --dry-run
[ "$STATUS" -ne 0 ] || fail "--dry-run on a bad --path exited 0"
if [ "$failures" -eq "$group_start" ]; then
  echo "ok: --dry-run valid exits 0, bad exits non-zero"
fi

# ─── Flag-shaped values are rejected at parse time (#951) ──────────────
# PR #939 made the value-taking options reject a flag-shaped next token
# (--path --dry-run used to print "not a directory: --dry-run" and exit
# per the bad-path rules above instead of failing at parse time), but
# added no test for it. Each option must exit 2 naming itself.
for opt in --path --depth --find --user; do
  run "$opt" --dry-run
  [ "$STATUS" -eq 2 ] || fail "$opt --dry-run exited $STATUS (want 2)"
  case "$ERR" in
    *"git-discover-repos: $opt requires a value"*) echo "ok: $opt --dry-run rejected, option named" ;;
    *) fail "$opt --dry-run did not name the option on stderr (got: $ERR)" ;;
  esac
done

SNAP_BEFORE="$(find "$TMP_ROOT" | sort)"
# ─── Missing values are rejected at parse time (#970) ──────────
# PR #921 made the value-taking options reject a missing value (option
# last, or an empty string) with exit 2, but no suite pinned that branch:
# a refactor dropping the -n check would pass the flag-shaped runs above
# while a trailing option silently took an empty value. Each option must
# exit 2 naming itself for both shapes.
for opt in --path --depth --find --user; do
  run "$opt"
  [ "$STATUS" -eq 2 ] || fail "$opt (trailing, no value) exited $STATUS (want 2)"
  case "$ERR" in
    *"git-discover-repos: $opt requires a value"*) echo "ok: $opt trailing rejected, option named" ;;
    *) fail "$opt (trailing, no value) did not name the option on stderr (got: $ERR)" ;;
  esac
  run "$opt" ""
  [ "$STATUS" -eq 2 ] || fail "$opt (empty value) exited $STATUS (want 2)"
  case "$ERR" in
    *"git-discover-repos: $opt requires a value"*) echo "ok: $opt empty value rejected, option named" ;;
    *) fail "$opt (empty value) did not name the option on stderr (got: $ERR)" ;;
  esac
done

if [ "$(find "$TMP_ROOT" | sort)" = "$SNAP_BEFORE" ]; then
  echo "ok: missing-value runs left the fixture tree unchanged"
else
  fail "missing-value runs changed the fixture tree"
fi

# ─── Repeated value options are rejected at parse time (#1120) ─────
# --depth/--find/--user used to assign unconditionally, so a repeat
# silently discarded the first value (a duplicated --user scanned a
# different home; a duplicated --find dropped a spec). Each repeat must
# exit 2 naming the option, adopt #1103 style. --path stays multi-valued.
for opt in --depth --find --user; do
  case "$opt" in
    --user) run --path "$VALID" --dry-run --user hatch --user root ;;
    *)      run --path "$VALID" --dry-run "$opt" 2 "$opt" 5 ;;
  esac
  [ "$STATUS" -eq 2 ] || fail "repeated $opt exited $STATUS (want 2)"
  case "$ERR" in
    *"git-discover-repos: $opt may be given only once"*) echo "ok: repeated $opt rejected, option named" ;;
    *) fail "repeated $opt did not name the option on stderr (got: $ERR)" ;;
  esac
done
run --path "$VALID" --path "$VALID" --dry-run
[ "$STATUS" -eq 0 ] || fail "repeated --path (multi-valued by design) exited $STATUS (want 0)"
echo "ok: repeated --path still accepted"

# ─── Conflicting filter flags are rejected at parse time (#1120) ────
# --repos-only/--worktrees-only/--both/--bare-only all assigned FILTER
# unconditionally, so any combination silently last-won.
for pair in "--repos-only --worktrees-only" "--worktrees-only --bare-only" "--both --repos-only" "--repos-only --repos-only"; do
  # shellcheck disable=SC2086
  run --path "$VALID" --dry-run $pair
  [ "$STATUS" -eq 2 ] || fail "filter pair '$pair' exited $STATUS (want 2)"
  case "$ERR" in
    *"may not be combined with"*) echo "ok: filter pair '$pair' rejected as a conflict" ;;
    *) fail "filter pair '$pair' was not named as a conflict on stderr (got: $ERR)" ;;
  esac
done
run --path "$VALID" --dry-run --repos-only
[ "$STATUS" -eq 0 ] || fail "single --repos-only exited $STATUS (want 0)"
echo "ok: single filter flag still accepted"

# ─── --follow with --no-follow is rejected at parse time (#1120) ────
# FOLLOW_SET was written but never read, so the pair silently resolved
# by order. Both orders must exit 2; each flag alone still parses.
for pair in "--follow --no-follow" "--no-follow --follow"; do
  # shellcheck disable=SC2086
  run --path "$VALID" --dry-run $pair
  [ "$STATUS" -eq 2 ] || fail "follow pair '$pair' exited $STATUS (want 2)"
  case "$ERR" in
    *"git-discover-repos: --follow and --no-follow may not be combined"*) echo "ok: follow pair '$pair' rejected as a conflict" ;;
    *) fail "follow pair '$pair' was not named as a conflict on stderr (got: $ERR)" ;;
  esac
done
run --path "$VALID" --dry-run --follow
[ "$STATUS" -eq 0 ] || fail "single --follow exited $STATUS (want 0)"
run --path "$VALID" --dry-run --no-follow
[ "$STATUS" -eq 0 ] || fail "single --no-follow exited $STATUS (want 0)"
echo "ok: single --follow / --no-follow still accepted"

[ "$failures" -eq 0 ] || { echo "$failures failure(s)" >&2; exit 1; }
echo "all git-discover-repos --path exit-status checks passed"
