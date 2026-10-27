#!/usr/bin/env bash
# Regression test for clean.sh's flag-shaped-value guards (#951).
#
# PR #939 (fix for #930) made clean.sh's value-taking options reject a
# flag-shaped next token — `--path --dry-run` used to swallow --dry-run
# as the path and run the cleanup for real — but added no test for
# clean.sh, so the guards could regress silently.
#
# The contract pinned here, for --path, --depth, and --docker-age:
#   <option> --dry-run  -> exit 2, "<option> requires a value" on
#                          stderr, and no side effects
#   valid values        -> accepted: the --only-unknown-scratch control
#                          run below parses all three options with real
#                          values and exits 0
#
# This drives the REAL clean.sh (no pasted copies) against temp dirs.
# The control uses --only-unknown-scratch so it exercises the parser and
# the post-parse judges, then sweeps only the isolated HOME's (empty)
# scratch root and exits — no docker/brew/git work runs.
set -uo pipefail

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$SELF_DIR/clean.sh"

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT
export HOME="$TMP_ROOT/home"
mkdir -p "$HOME"
# The control sweeps $HOME/.local/share/opencode/tmp; keep it absent so
# the sweep is a no-op regardless of the caller's real home.
unset DOCKER_AGE_DAYS DEPTH_LIMIT

failures=0
fail() {
  echo "FAIL: $1" >&2
  failures=$((failures + 1))
}

VALID="$TMP_ROOT/valid"
mkdir -p "$VALID"

run() { # run <args...> — sets OUT, ERR, STATUS
  OUT="$(bash "$SCRIPT" "$@" 2>"$TMP_ROOT/err")"; STATUS=$?
  ERR="$(cat "$TMP_ROOT/err")"
}

snapshot() { find "$VALID" "$HOME" | sort; }
BEFORE="$(snapshot)"

# ─── Flag-shaped values are rejected at parse time ─────────────────────
for opt in --path --depth --docker-age; do
  run "$opt" --dry-run
  [ "$STATUS" -eq 2 ] || fail "$opt --dry-run exited $STATUS (want 2)"
  case "$ERR" in
    *"clean.sh: $opt requires a value"*) echo "ok: $opt --dry-run rejected, option named" ;;
    *) fail "$opt --dry-run did not name the option on stderr (got: $ERR)" ;;
  esac
done

# ─── No side effects from the rejected runs ────────────────────────────
if [ "$(snapshot)" = "$BEFORE" ]; then
  echo "ok: rejected runs left the fixture tree unchanged"
else
  fail "rejected runs changed the fixture tree"
fi

# ─── Valid-value control ───────────────────────────────────────────────
run --path "$VALID" --depth 2 --docker-age 7 --only-unknown-scratch
[ "$STATUS" -eq 0 ] || fail "valid-value control exited $STATUS (stderr: $ERR)"
case "$OUT" in
  *"nothing >=500MB"*) echo "ok: valid values parse and the control run exits 0" ;;
  *) fail "valid-value control output changed (got: $OUT)" ;;
esac

[ "$failures" -eq 0 ] || { echo "$failures failure(s)" >&2; exit 1; }
echo "all clean.sh flag-shaped-value checks passed"
