#!/usr/bin/env bash
# Regression test for git-maintain-repos's flag-shaped-value guards (#951).
#
# PR #939 (fix for #930) made git-maintain-repos's value-taking options
# reject a flag-shaped next token — `--path --dry-run` used to swallow
# --dry-run as the root and fail later as "not a directory" (or worse,
# proceed) — but added no test for git-maintain-repos, so the guards
# could regress silently.
#
# The contract pinned here, for --path, --depth, --jobs, --pack-threads,
# and --reflog-expire:
#   <option> --dry-run  -> exit 2, "<option> requires a value" on
#                          stderr, and no side effects
#   valid values        -> accepted: the --dry-run control run below
#                          parses all five options with real values,
#                          discovers the fixture repo, and exits 0
#
# This drives the REAL git-maintain-repos (no pasted copies) against
# temp dirs. The fixture repo is a plain directory with a .git
# directory, which is all discovery looks for. git-maintain-repos
# shells out to the sibling git-discover-repos, so the suite must run
# from a tree that has both scripts and lib/common.sh (the repo root,
# or the flake check's copies).
set -uo pipefail

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$SELF_DIR/git-maintain-repos"
# Shared conformance probe matrix helpers (#1146, design #1145 phase 1).
# shellcheck source=lib/arg-probe.sh
. "$SELF_DIR/lib/arg-probe.sh"


TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT
export HOME="$TMP_ROOT/home"
mkdir -p "$HOME"
unset JOBS PACK_THREADS REFLOG_EXPIRE DEPTH SCOPE

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

snapshot() { find "$VALID" "$HOME" | sort; }
BEFORE="$(snapshot)"

# ─── Flag-shaped values are rejected at parse time ─────────────────────
for opt in --path --depth --jobs --pack-threads --reflog-expire; do
  run "$opt" --dry-run
  [ "$STATUS" -eq 2 ] || fail "$opt --dry-run exited $STATUS (want 2)"
  case "$ERR" in
    *"git-maintain-repos: $opt requires a value"*) echo "ok: $opt --dry-run rejected, option named" ;;
    *) fail "$opt --dry-run did not name the option on stderr (got: $ERR)" ;;
  esac
done

# ─── Missing values are rejected at parse time (#970) ──────────
# PR #921 made the value-taking options reject a missing value (option
# last, or an empty string) with exit 2, but no suite pinned that branch:
# a refactor dropping the -n check would pass the flag-shaped runs above
# while a trailing option silently took an empty value. Each option must
# exit 2 naming itself for both shapes.
for opt in --path --depth --jobs --pack-threads --reflog-expire; do
  run "$opt"
  [ "$STATUS" -eq 2 ] || fail "$opt (trailing, no value) exited $STATUS (want 2)"
  case "$ERR" in
    *"git-maintain-repos: $opt requires a value"*) echo "ok: $opt trailing rejected, option named" ;;
    *) fail "$opt (trailing, no value) did not name the option on stderr (got: $ERR)" ;;
  esac
  run "$opt" ""
  [ "$STATUS" -eq 2 ] || fail "$opt (empty value) exited $STATUS (want 2)"
  case "$ERR" in
    *"git-maintain-repos: $opt requires a value"*) echo "ok: $opt empty value rejected, option named" ;;
    *) fail "$opt (empty value) did not name the option on stderr (got: $ERR)" ;;
  esac
done

# ─── No side effects from the rejected runs ────────────────────────────
if [ "$(snapshot)" = "$BEFORE" ]; then
  echo "ok: rejected runs left the fixture tree unchanged"
else
  fail "rejected runs changed the fixture tree"
fi

# ─── Valid-value control ───────────────────────────────────────────────
run --path "$VALID" --depth 2 --jobs 1 --pack-threads 1 --reflog-expire 1.day.ago --dry-run
[ "$STATUS" -eq 0 ] || fail "valid-value control exited $STATUS (stderr: $ERR)"
case "$OUT" in
  *"1 repos"*) echo "ok: valid values parse, fixture repo discovered, exits 0" ;;
  *) fail "valid-value control did not report 1 repo (got: $OUT)" ;;
esac

# ─── Second target is rejected at parse time (#1121) ───────────────
# --path used to assign ROOT unconditionally, so a second target in any
# form silently discarded the first (the positional branch already
# guarded this; --path did not). Every shape must exit 2 naming the
# unexpected argument, #1111 style.
OTHER="$TMP_ROOT/other"
mkdir -p "$OTHER"
run --dry-run --path "$VALID" --path "$OTHER"
[ "$STATUS" -eq 2 ] || fail "repeated --path exited $STATUS (want 2)"
case "$ERR" in
  *"unexpected argument: --path"*) echo "ok: repeated --path rejected, option named" ;;
  *) fail "repeated --path did not name the option on stderr (got: $ERR)" ;;
esac
run --dry-run "$VALID" --path "$OTHER"
[ "$STATUS" -eq 2 ] || fail "positional then --path exited $STATUS (want 2)"
case "$ERR" in
  *"unexpected argument: --path"*) echo "ok: --path after positional rejected, option named" ;;
  *) fail "--path after positional did not name the option on stderr (got: $ERR)" ;;
esac
run --dry-run --path "$VALID" "$OTHER"
[ "$STATUS" -eq 2 ] || fail "--path then positional exited $STATUS (want 2)"
case "$ERR" in
  *"unexpected argument: $OTHER"*) echo "ok: positional after --path rejected, argument named" ;;
  *) fail "positional after --path did not name the argument on stderr (got: $ERR)" ;;
esac

# ─── Mode-flag conflicts are rejected at parse time (#1121) ────────
# --quick/--safe/--deep used to overwrite DEPTH unconditionally, so
# --quick --deep silently escalated to the destructive deep pass.
for pair in "--quick --deep" "--deep --quick" "--safe --deep" "--quick --quick"; do
  # shellcheck disable=SC2086
  run --dry-run --path "$VALID" $pair
  [ "$STATUS" -eq 2 ] || fail "mode pair '$pair' exited $STATUS (want 2)"
  case "$ERR" in
    *"may not be combined with"*) echo "ok: mode pair '$pair' rejected as a conflict" ;;
    *) fail "mode pair '$pair' was not named as a conflict on stderr (got: $ERR)" ;;
  esac
done
run --dry-run --path "$VALID" --quick
[ "$STATUS" -eq 0 ] || fail "single --quick exited $STATUS (want 0)"
echo "ok: single mode flag still accepted"

# ─── Repeated value options are rejected at parse time (#1121) ─────
for opt in --depth --jobs --pack-threads --reflog-expire; do
  case "$opt" in
    --reflog-expire) run --dry-run --path "$VALID" "$opt" 1.day.ago "$opt" 2.days.ago ;;
    *)               run --dry-run --path "$VALID" "$opt" 1 "$opt" 2 ;;
  esac
  [ "$STATUS" -eq 2 ] || fail "repeated $opt exited $STATUS (want 2)"
  case "$ERR" in
    *"git-maintain-repos: $opt may be given only once"*) echo "ok: repeated $opt rejected, option named" ;;
    *) fail "repeated $opt did not name the option on stderr (got: $ERR)" ;;
  esac
done

# ─── --system with --no-system is rejected at parse time (#1121) ───
for pair in "--system --no-system" "--no-system --system"; do
  # shellcheck disable=SC2086
  run --dry-run --path "$VALID" $pair
  [ "$STATUS" -eq 2 ] || fail "scope pair '$pair' exited $STATUS (want 2)"
  case "$ERR" in
    *"git-maintain-repos: --system and --no-system may not be combined"*) echo "ok: scope pair '$pair' rejected as a conflict" ;;
    *) fail "scope pair '$pair' was not named as a conflict on stderr (got: $ERR)" ;;
  esac
done
run --dry-run --path "$VALID" --system
[ "$STATUS" -eq 0 ] || fail "single --system exited $STATUS (want 0)"
echo "ok: single --system still accepted"

# --- --follow/filter pairs are rejected at parse time (#1135) ---
# git-discover-repos rejected these in #1123, but git-maintain-repos
# assigned FOLLOW and FILTER unconditionally, so the last flag silently
# won (the filter decides which repos get gc'd).
for pair in "--follow --no-follow" "--no-follow --follow"; do
  # shellcheck disable=SC2086
  run --dry-run --path "$VALID" $pair
  [ "$STATUS" -eq 2 ] || fail "follow pair '$pair' exited $STATUS (want 2)"
  case "$ERR" in
    *"git-maintain-repos: --follow and --no-follow may not be combined"*) echo "ok: follow pair '$pair' rejected as a conflict" ;;
    *) fail "follow pair '$pair' was not named as a conflict on stderr (got: $ERR)" ;;
  esac
done
for pair in "--repos-only --worktrees-only" "--worktrees-only --repos-only"; do
  # shellcheck disable=SC2086
  run --dry-run --path "$VALID" $pair
  [ "$STATUS" -eq 2 ] || fail "filter pair '$pair' exited $STATUS (want 2)"
  case "$ERR" in
    *"may not be combined with"*"only one filter option may be given"*) echo "ok: filter pair '$pair' rejected as a conflict" ;;
    *) fail "filter pair '$pair' was not named as a conflict on stderr (got: $ERR)" ;;
  esac
done
for flag in --follow --no-follow --repos-only --worktrees-only; do
  run --dry-run --path "$VALID" "$flag"
  [ "$STATUS" -eq 0 ] || fail "single $flag exited $STATUS (want 0)"
  echo "ok: single $flag still accepted"
done

# ─── --no-recursive --follow is rejected at parse time (#1268) ─────
# git-discover-repos rejects this pair at exit 2, but maintain used to
# forward both and read discovery through a process substitution with
# stderr discarded, so the failure looked like an empty tree:
# "0 repos" at exit 0. Both orders must now exit 2 naming the pair.
for pair in "--no-recursive --follow" "--follow --no-recursive"; do
  # shellcheck disable=SC2086
  run --dry-run --path "$VALID" $pair
  [ "$STATUS" -eq 2 ] || fail "no-recursive/follow pair '$pair' exited $STATUS (want 2)"
  case "$ERR" in
    *"git-maintain-repos: --no-recursive conflicts with --follow"*) echo "ok: pair '$pair' rejected as a conflict" ;;
    *) fail "pair '$pair' was not named as a conflict on stderr (got: $ERR)" ;;
  esac
done
# Control: --no-recursive alone still discovers the fixture repo
# (depth 1 matches the repo at the path itself).
run --dry-run --path "$VALID/repo1" --no-recursive
[ "$STATUS" -eq 0 ] || fail "--no-recursive control exited $STATUS (want 0)"
case "$OUT" in
  *"1 repos"*) echo "ok: --no-recursive control discovers the fixture repo" ;;
  *) fail "--no-recursive control did not report 1 repo (got: $OUT)" ;;
esac

# ─── Shared conformance probe matrix (#1146, design #1145) ─────────
# The matrix rows for git-maintain-repos, driven through
# lib/arg-probe.sh (the same helper every discovery suite uses):
# unknown, missing/flag-shaped (also pinned per-option above), repeat,
# both conflict orders, extra positional, and bad numbers -- abc, 0,
# oversized -- for --depth/--jobs. --pack-threads takes the same rows
# except 0, which is legal there (git auto-detect), so 0 is probed as
# accepted; 08 is probed as accepted decimal 8 (rule 6, #881).
probe_rejected "unknown option: --bogus" --bogus
probe_rejected "git-maintain-repos: --jobs requires a value" --jobs
probe_rejected "git-maintain-repos: --jobs requires a value" --jobs --dry-run
probe_rejected "git-maintain-repos: --jobs may be given only once" --dry-run --path "$VALID" --jobs 1 --jobs 2
probe_rejected "may not be combined with" --dry-run --path "$VALID" --quick --deep
probe_rejected "may not be combined with" --dry-run --path "$VALID" --deep --quick
probe_rejected "git-maintain-repos: --system and --no-system may not be combined" --dry-run --path "$VALID" --system --no-system
probe_rejected "git-maintain-repos: --system and --no-system may not be combined" --dry-run --path "$VALID" --no-system --system
probe_rejected "unexpected argument: $OTHER" --dry-run "$VALID" "$OTHER"
probe_rejected "git-maintain-repos: empty positional argument (target must not be empty)" --dry-run ""
for bad in abc 0 2147483648 99999999999999999999999999; do
  probe_rejected "git-maintain-repos: --depth must be a positive integer" --dry-run --path "$VALID" --depth "$bad"
  probe_rejected "git-maintain-repos: --jobs must be a positive integer" --dry-run --path "$VALID" --jobs "$bad"
done
for bad in abc 2147483648 99999999999999999999999999; do
  probe_rejected "git-maintain-repos: --pack-threads must be a non-negative integer" --dry-run --path "$VALID" --pack-threads "$bad"
done
probe_accepted --dry-run --path "$VALID" --pack-threads 0
probe_accepted --dry-run --path "$VALID" --jobs 08 --depth 08

[ "$failures" -eq 0 ] || { echo "$failures failure(s)" >&2; exit 1; }
echo "all git-maintain-repos flag-shaped-value checks passed"
