#!/usr/bin/env bash
# Regression test for spotlight-exclude-artifacts's flag-shaped-value
# guards (#951).
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
