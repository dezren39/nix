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

[ "$failures" -eq 0 ] || { echo "$failures failure(s)" >&2; exit 1; }
echo "all git-maintain-repos flag-shaped-value checks passed"
