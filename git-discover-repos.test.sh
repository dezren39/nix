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
# #1325 adds the enumeration-failure contract: a find that fails mid-walk
# (an unreadable subdirectory under an explicitly given --path) is named
# on stderr and the script exits non-zero, while the repos that were
# found are still printed. The --find artifact sweep surfaces its own
# enumeration failures the same way. --auto stays best-effort.
#
# #1343 adds the follow-loop contract: a failed `git worktree list`
# or `rev-parse --git-common-dir` in the --follow loop is named on
# stderr and the script exits non-zero, while the entries that were
# still determined are printed.
#
# This drives the REAL git-discover-repos (no pasted copies) against temp
# dirs. The fixture repo is a plain directory with a .git directory, which
# is all discovery's classify() looks for — no git binary needed.
set -uo pipefail

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$SELF_DIR/git-discover-repos"
# Shared conformance probe matrix helpers (#1146, design #1145 phase 1).
# shellcheck source=lib/arg-probe.sh
. "$SELF_DIR/lib/arg-probe.sh"


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

# ─── --auto with --path/positional is rejected at parse time (#1584) ────
# Usage documents --auto as "Conflicts with --path", but the parser set
# MODE unconditionally, so the pair silently resolved by last-win: the
# --path-then---auto order discarded the named path and scanned the
# whole machine at exit 0. All three orders must exit 2 naming both
# flags, before any discovery (probed here under --dry-run, which must
# fail the same way instead of printing a plan); single-mode controls
# are unchanged.
run --path "$VALID" --dry-run --auto
[ "$STATUS" -eq 2 ] || fail "--path then --auto exited $STATUS (want 2)"
case "$ERR" in
  *"git-discover-repos: --auto and --path may not be combined"*) echo "ok: --path then --auto rejected as a conflict" ;;
  *) fail "--path then --auto did not name both flags on stderr (got: $ERR)" ;;
esac
run --auto --path "$VALID" --dry-run
[ "$STATUS" -eq 2 ] || fail "--auto then --path exited $STATUS (want 2)"
case "$ERR" in
  *"git-discover-repos: --auto and --path may not be combined"*) echo "ok: --auto then --path rejected as a conflict" ;;
  *) fail "--auto then --path did not name both flags on stderr (got: $ERR)" ;;
esac
run "$VALID" --auto --dry-run
[ "$STATUS" -eq 2 ] || fail "positional then --auto exited $STATUS (want 2)"
case "$ERR" in
  *"git-discover-repos: --auto and --path may not be combined"*) echo "ok: positional then --auto rejected as a conflict" ;;
  *) fail "positional then --auto did not name both flags on stderr (got: $ERR)" ;;
esac
run --auto "$VALID" --dry-run
[ "$STATUS" -eq 2 ] || fail "--auto then positional exited $STATUS (want 2)"
case "$ERR" in
  *"git-discover-repos: --auto and --path may not be combined"*) echo "ok: --auto then positional rejected as a conflict" ;;
  *) fail "--auto then positional did not name both flags on stderr (got: $ERR)" ;;
esac
run --auto --dry-run
[ "$STATUS" -eq 0 ] || fail "single --auto exited $STATUS (want 0)"
case "$OUT" in *"mode:       auto"*) echo "ok: single --auto still accepted" ;; *) fail "single --auto plan changed (got: $OUT)" ;; esac
run "$VALID" --dry-run
[ "$STATUS" -eq 0 ] || fail "single positional path exited $STATUS (want 0)"
echo "ok: single positional path still accepted"

# ─── Enumeration failure under --path is not swallowed (#1325) ─────
# A find shim fails only for one named scan root, regardless of the
# privileges the suite runs with (a chmod 000 fixture does not stop
# root — the #1257 lesson, same pattern as var-folders-clean's #1321
# shim). The shim first emits the real find's partial output and only
# then fails, like a real find that walked part of the tree before
# hitting the unreadable directory.
PARTIAL="$TMP_ROOT/partial"
mkdir -p "$PARTIAL/repoA/.git" "$PARTIAL/locked/repoB/.git"
PARTIAL_CANON="$(cd "$PARTIAL" && pwd -P)"
find_shim_dir="$TMP_ROOT/find-shim"
mkdir -p "$find_shim_dir"
real_find="$(command -v find)"
make_find_shim() {
  # $1 = the exact path argument the shimmed find fails on
  cat > "$find_shim_dir/find" <<SHIM_EOF
#!/bin/sh
for arg in "\$@"; do
  if [ "\$arg" = "$1" ]; then
    $real_find "\$@" 2>/dev/null
    echo "find: $1/locked: Permission denied (shimmed failure)" >&2
    exit 1
  fi
done
exec $real_find "\$@"
SHIM_EOF
  chmod +x "$find_shim_dir/find"
}

make_find_shim "$PARTIAL_CANON"
OUT="$(PATH="$find_shim_dir:$PATH" "$SCRIPT" --path "$PARTIAL" 2>"$TMP_ROOT/err")"; STATUS=$?
ERR="$(cat "$TMP_ROOT/err")"
[ "$STATUS" -ne 0 ] || fail "enumeration failure under --path exited 0 (#1325)"
case "$ERR" in *"failed to enumerate: $PARTIAL_CANON"*) echo "ok: enumeration failure named on stderr" ;; *) fail "enumeration failure not named on stderr (got: $ERR)" ;; esac
case "$OUT" in *"repo"$'\t'"$PARTIAL_CANON/repoA"*) echo "ok: repos found before the failure still printed" ;; *) fail "partial run did not list repoA (got: $OUT)" ;; esac

# ─── --find sweep surfaces its own enumeration failures (#1325) ─────
# Same shim, keyed on the discovered repo's path — the only find the
# sweep runs is on each discovered location, so a failure there is the
# sweep's own, not seeding's.
SWEEP="$TMP_ROOT/sweep"
mkdir -p "$SWEEP/repoA/.git" "$SWEEP/repoA/.trash"
SWEEP_CANON="$(cd "$SWEEP" && pwd -P)"
make_find_shim "$SWEEP_CANON/repoA"
OUT="$(PATH="$find_shim_dir:$PATH" "$SCRIPT" --path "$SWEEP" --find trash 2>"$TMP_ROOT/err")"; STATUS=$?
ERR="$(cat "$TMP_ROOT/err")"
[ "$STATUS" -ne 0 ] || fail "--find sweep enumeration failure exited 0 (#1325)"
case "$ERR" in *"failed to enumerate artifacts under: $SWEEP_CANON/repoA"*) echo "ok: sweep enumeration failure named on stderr" ;; *) fail "sweep enumeration failure not named on stderr (got: $ERR)" ;; esac
case "$OUT" in *"FOUND"$'\t'"trash"*) echo "ok: sweep hits found before the failure still printed" ;; *) fail "sweep partial run did not print its FOUND hit (got: $OUT)" ;; esac
rm -f "$find_shim_dir/find"

# ─── Real unreadable subdirectory (the #1325 probe, non-root only) ──
# chmod 000 does not stop root, so this real-fixture check runs only
# for a non-root suite; the shim cases above pin the contract either
# way.
if [ "$(id -u)" -ne 0 ]; then
  LOCKED="$TMP_ROOT/locked"
  mkdir -p "$LOCKED/repoA/.git" "$LOCKED/locked/repoB/.git"
  LOCKED_CANON="$(cd "$LOCKED" && pwd -P)"
  chmod 000 "$LOCKED/locked"
  run --path "$LOCKED"
  chmod 755 "$LOCKED/locked"
  [ "$STATUS" -ne 0 ] || fail "unreadable subdirectory under --path exited 0 (#1325)"
  case "$ERR" in *"$LOCKED_CANON/locked"*) echo "ok: unreadable directory named on stderr" ;; *) fail "unreadable directory not named on stderr (got: $ERR)" ;; esac
  case "$OUT" in *"repo"$'\t'"$LOCKED_CANON/repoA"*) echo "ok: readable repo still listed beside the unreadable directory" ;; *) fail "unreadable-dir run did not list repoA (got: $OUT)" ;; esac
fi

# ─── --follow git failures are not swallowed (#1343) ─────────────
# The follow loop's two git calls used to discard status and stderr
# (rev-parse with 2>/dev/null; worktree list inside a process
# substitution, whose status cannot reach the script), so a failed
# enumeration read as "no links": linked worktrees vanished and the
# run exited 0. A git shim that delegates everything except one named
# subcommand pins the contract: the failure is named on stderr (the
# directory and git's own error), the run exits non-zero, and the
# entries that were still determined are printed. This section needs
# real repositories (following speaks real git), unlike the fake .git
# fixtures above.
FOLLOWED="$TMP_ROOT/followed"
mkdir -p "$FOLLOWED"
real_git="$(command -v git)"
"$real_git" init -q "$FOLLOWED/repo"
printf 'x\n' > "$FOLLOWED/repo/file"
"$real_git" -C "$FOLLOWED/repo" add file
"$real_git" -C "$FOLLOWED/repo" -c user.name=Test -c user.email=test@example.com commit -qm init
"$real_git" -C "$FOLLOWED/repo" worktree add -q "$FOLLOWED/linked"
# Git permits a linked worktree checkout inside .git; discovery deliberately
# traverses .git trees so these worktrees remain visible.
"$real_git" -C "$FOLLOWED/repo" worktree add -q "$FOLLOWED/repo/.git/embedded"
FOLLOWED_CANON="$(cd "$FOLLOWED" && pwd -P)"

run --path "$FOLLOWED/repo"
[ "$STATUS" -eq 0 ] || fail "plain scan with an embedded worktree exited $STATUS"
case "$OUT" in *"worktree"$'\t'"$FOLLOWED_CANON/repo/.git/embedded"*) echo "ok: plain scan discovers the worktree embedded in .git" ;; *) fail "plain scan missed the worktree embedded in .git (got: $OUT)" ;; esac

run --path "$FOLLOWED/repo" --follow
[ "$STATUS" -eq 0 ] || fail "healthy --follow exited $STATUS (#1343)"
case "$OUT" in *"repo"$'\t'"$FOLLOWED_CANON/repo"*) echo "ok: healthy --follow lists the repo" ;; *) fail "healthy --follow did not list the repo (got: $OUT)" ;; esac
case "$OUT" in *"worktree"$'\t'"$FOLLOWED_CANON/linked"*) echo "ok: healthy --follow lists the linked worktree" ;; *) fail "healthy --follow did not list the linked worktree (got: $OUT)" ;; esac
case "$OUT" in *"worktree"$'\t'"$FOLLOWED_CANON/repo/.git/embedded"*) echo "ok: healthy --follow lists the worktree embedded in .git" ;; *) fail "healthy --follow missed the worktree embedded in .git (got: $OUT)" ;; esac

git_shim_dir="$TMP_ROOT/git-shim"
mkdir -p "$git_shim_dir"
make_git_shim() {
  # $1 = the subcommand words the shimmed git fails, e.g. "worktree list"
  cat > "$git_shim_dir/git" <<SHIM_EOF
#!/bin/sh
case " \$* " in
  *" $1 "*) echo "fatal: stubbed $1 failure" >&2; exit 128 ;;
esac
exec $real_git "\$@"
SHIM_EOF
  chmod +x "$git_shim_dir/git"
}

make_git_shim "worktree list"
OUT="$(PATH="$git_shim_dir:$PATH" "$SCRIPT" --path "$FOLLOWED/repo" --follow 2>"$TMP_ROOT/err")"; STATUS=$?
ERR="$(cat "$TMP_ROOT/err")"
[ "$STATUS" -ne 0 ] || fail "failed worktree list under --follow exited 0 (#1343)"
case "$ERR" in *"failed to list worktrees for: $FOLLOWED_CANON/repo"*) echo "ok: worktree-list failure named on stderr" ;; *) fail "worktree-list failure not named on stderr (got: $ERR)" ;; esac
case "$ERR" in *"fatal: stubbed worktree list failure"*) echo "ok: git's own worktree-list error surfaced on stderr" ;; *) fail "git's own worktree-list error not surfaced (got: $ERR)" ;; esac
case "$OUT" in *"repo"$'\t'"$FOLLOWED_CANON/repo"*) echo "ok: repo still printed when worktree list fails" ;; *) fail "worktree-list failure run did not list the repo (got: $OUT)" ;; esac

make_git_shim "rev-parse"
OUT="$(PATH="$git_shim_dir:$PATH" "$SCRIPT" --path "$FOLLOWED/repo" --follow 2>"$TMP_ROOT/err")"; STATUS=$?
ERR="$(cat "$TMP_ROOT/err")"
[ "$STATUS" -ne 0 ] || fail "failed rev-parse under --follow exited 0 (#1343)"
case "$ERR" in *"failed to resolve git common dir for: $FOLLOWED_CANON/repo"*) echo "ok: rev-parse failure named on stderr" ;; *) fail "rev-parse failure not named on stderr (got: $ERR)" ;; esac
case "$OUT" in *"worktree"$'\t'"$FOLLOWED_CANON/linked"*) echo "ok: linked worktree still printed when rev-parse fails" ;; *) fail "rev-parse failure run did not list the linked worktree (got: $OUT)" ;; esac
rm -f "$git_shim_dir/git"

# ─── Submodule classification (#1511) ─────────────────────────────
# A submodule checkout has a .git FILE just like a linked worktree,
# but its gitdir pointer targets <super>/.git/modules/<name> and it
# owns an independent object store. classify() used to call every
# .git file a worktree, so --path mode never maintained the
# submodule's store, and the --follow hop followed the common dir
# into .git/modules and emitted a repo record inside another repo's
# .git. Pinned here with a real superproject + submodule fixture:
# the checkout is a repo exactly once in both modes, no record
# points inside .git, and the linked-worktree section above pins
# that real worktrees are unchanged.
SUBM="$TMP_ROOT/submod"
mkdir -p "$SUBM"
export GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=protocol.file.allow GIT_CONFIG_VALUE_0=always
"$real_git" init -q "$SUBM/sub-src"
printf 'x\n' > "$SUBM/sub-src/file"
"$real_git" -C "$SUBM/sub-src" add file
"$real_git" -C "$SUBM/sub-src" -c user.name=Test -c user.email=test@example.com commit -qm init
"$real_git" init -q "$SUBM/super"
"$real_git" -C "$SUBM/super" submodule add -q ../sub-src sub
"$real_git" -C "$SUBM/super" add .gitmodules sub
"$real_git" -C "$SUBM/super" -c user.name=Test -c user.email=test@example.com commit -qm add-sub
unset GIT_CONFIG_COUNT GIT_CONFIG_KEY_0 GIT_CONFIG_VALUE_0
SUBM_CANON="$(cd "$SUBM" && pwd -P)"

run --path "$SUBM/super"
[ "$STATUS" -eq 0 ] || fail "submodule --path exited $STATUS (#1511)"
case "$OUT" in *"repo"$'\t'"$SUBM_CANON/super"*) echo "ok: --path lists the superproject as a repo" ;; *) fail "--path did not list the superproject as a repo (got: $OUT)" ;; esac
case "$OUT" in *"repo"$'\t'"$SUBM_CANON/super/sub"*) echo "ok: --path lists the submodule checkout as a repo (#1511)" ;; *) fail "--path did not list the submodule checkout as a repo (got: $OUT)" ;; esac
case "$OUT" in *"worktree"*) fail "--path labelled the submodule a worktree (got: $OUT)" ;; *) echo "ok: --path emits no worktree record for the submodule" ;; esac
case "$OUT" in *".git/modules"*) fail "--path emitted a record inside .git/modules (got: $OUT)" ;; *) echo "ok: --path emits no record inside .git" ;; esac

run --path "$SUBM/super" --follow
[ "$STATUS" -eq 0 ] || fail "submodule --follow exited $STATUS (#1511)"
case "$OUT" in *"repo"$'\t'"$SUBM_CANON/super/sub"*) echo "ok: --follow lists the submodule checkout as a repo (#1511)" ;; *) fail "--follow did not list the submodule checkout as a repo (got: $OUT)" ;; esac
case "$OUT" in *".git/modules"*) fail "--follow emitted a record inside .git/modules (got: $OUT)" ;; *) echo "ok: --follow emits no record inside .git" ;; esac
case "$OUT" in *"worktree"*) fail "--follow labelled the submodule a worktree (got: $OUT)" ;; *) echo "ok: --follow emits no worktree record for the submodule" ;; esac

# ─── Nested bare repositories are found and pruned (#1587) ──────────
# Bare repositories have no .git marker: the repository directory itself
# has HEAD, objects/, and refs/. The scan must report that directory and
# prune it before walking the object store.
BARE_SCAN="$TMP_ROOT/bare-scan"
BARE_REPO="$BARE_SCAN/bare.git"
NORMAL_REPO="$BARE_SCAN/normal"
DEEP_BARE="$BARE_SCAN/nested/deep.git"
DECOY="$BARE_SCAN/not-bare"
mkdir -p "$NORMAL_REPO/.git" "$BARE_SCAN/nested" "$DECOY"
git init --bare -q "$BARE_REPO"
git init --bare -q "$DEEP_BARE"
# A repo-shaped marker inside the object store must not become a result.
mkdir -p "$BARE_REPO/objects/probe/.git"
# Names alone are not enough: objects/ and refs/ must be directories.
printf 'not a bare repo\n' > "$DECOY/HEAD"
printf 'not a directory\n' > "$DECOY/objects"
printf 'not a directory\n' > "$DECOY/refs"
BARE_SCAN_CANON="$(cd "$BARE_SCAN" && pwd -P)"
BARE_REPO_CANON="$BARE_SCAN_CANON/bare.git"
NORMAL_REPO_CANON="$BARE_SCAN_CANON/normal"
DEEP_BARE_CANON="$BARE_SCAN_CANON/nested/deep.git"

run --path "$BARE_SCAN"
[ "$STATUS" -eq 0 ] || fail "nested-bare plain scan exited $STATUS (#1587)"
case "$OUT" in *$'repo\t'"$BARE_REPO_CANON"*) echo "ok: plain scan lists the nested bare repo (#1587)" ;; *) fail "plain scan missed the nested bare repo (got: $OUT)" ;; esac
case "$OUT" in *$'repo\t'"$NORMAL_REPO_CANON"*) echo "ok: plain scan still lists a normal repo beside the bare repo" ;; *) fail "plain scan missed the normal repo (got: $OUT)" ;; esac
case "$OUT" in *"$BARE_REPO_CANON/objects/probe"*) fail "plain scan descended into the bare repo object store (got: $OUT)" ;; *) echo "ok: plain scan prunes the bare repo object store" ;; esac
case "$OUT" in *"$DECOY"*) fail "plain scan mistook marker names without the bare-repo shape (got: $OUT)" ;; *) echo "ok: marker names without the bare-repo shape are ignored" ;; esac
bare_record_count="$(printf '%s\n' "$OUT" | grep -Fxc $'repo\t'"$BARE_REPO_CANON" || true)"
[ "$bare_record_count" -eq 1 ] || fail "plain scan emitted the direct bare repo $bare_record_count times (want 1)"

run --path "$BARE_SCAN" --bare-only
[ "$STATUS" -eq 0 ] || fail "nested-bare --bare-only scan exited $STATUS (#1587)"
expected_bare_only="$(printf 'repo\t%s\nrepo\t%s' "$BARE_REPO_CANON" "$DEEP_BARE_CANON")"
[ "$OUT" = "$expected_bare_only" ] || fail "--bare-only did not list exactly the two nested bare repos (got: $OUT)"

run --path "$BARE_SCAN" --no-bare
[ "$STATUS" -eq 0 ] || fail "nested-bare --no-bare scan exited $STATUS (#1587)"
[ "$OUT" = $'repo\t'"$NORMAL_REPO_CANON" ] || fail "--no-bare did not list only the normal repo (got: $OUT)"

run --path "$BARE_SCAN" --depth 1 --bare-only
[ "$STATUS" -eq 0 ] || fail "nested-bare --depth 1 scan exited $STATUS (#1587)"
[ "$OUT" = $'repo\t'"$BARE_REPO_CANON" ] || fail "--depth 1 did not limit discovery to the direct-child bare repo (got: $OUT)"
run --path "$BARE_SCAN" --depth 2 --bare-only
[ "$STATUS" -eq 0 ] || fail "nested-bare --depth 2 scan exited $STATUS (#1587)"
[ "$OUT" = "$expected_bare_only" ] || fail "--depth 2 did not include the deeper bare repo (got: $OUT)"
run --path "$BARE_SCAN" --depth 2147483647 --bare-only
[ "$STATUS" -eq 0 ] || fail "nested-bare maximum --depth scan exited $STATUS (#1587)"
[ "$OUT" = "$expected_bare_only" ] || fail "maximum --depth lost the nested bare repos (got: $OUT)"

# A bare repo that is also an explicit seed and a --follow target is emitted
# once, just like a nested bare repo reached by the recursive scan.
run --path "$BARE_REPO" --path "$BARE_REPO" --follow --bare-only
[ "$STATUS" -eq 0 ] || fail "direct bare-repo --follow control exited $STATUS (#1587)"
bare_record_count="$(printf '%s\n' "$OUT" | grep -Fxc $'repo\t'"$BARE_REPO_CANON" || true)"
[ "$bare_record_count" -eq 1 ] || fail "direct/follow bare repo emitted $bare_record_count times (want 1; got: $OUT)"

run --path "$BARE_REPO"
[ "$STATUS" -eq 0 ] || fail "direct --path bare control exited $STATUS (#1587)"
[ "$OUT" = $'repo\t'"$BARE_REPO_CANON" ] || fail "direct --path bare output changed (got: $OUT)"
run --path "$BARE_REPO" --bare-only
[ "$STATUS" -eq 0 ] || fail "direct --path bare --bare-only control exited $STATUS (#1587)"
[ "$OUT" = $'repo\t'"$BARE_REPO_CANON" ] || fail "direct --path bare --bare-only changed (got: $OUT)"
run --path "$BARE_REPO" --no-bare
[ "$STATUS" -eq 0 ] || fail "direct --path bare --no-bare control exited $STATUS (#1587)"
[ -z "$OUT" ] || fail "direct --path bare --no-bare printed a result (got: $OUT)"
echo "ok: nested and direct bare-repository discovery filters and pruning (#1587)"

# ─── Shared conformance probe matrix (#1146, design #1145) ─────────
# The matrix rows for git-discover-repos, driven through
# lib/arg-probe.sh (the same helper every discovery suite uses):
# unknown, missing/flag-shaped (also pinned per-option above), repeat,
# both conflict orders, and bad numbers -- abc, 0, oversized -- for
# --depth. Extra positionals are not a row here by design: positional
# paths are this script'''s declared repeatable input and accumulate.
# 08 is probed as accepted decimal 8 (rule 6, #881).
probe_rejected "unknown option: --bogus" --bogus
probe_rejected "git-discover-repos: --find requires a value" --find
probe_rejected "git-discover-repos: --find requires a value" --find --dry-run
probe_rejected "git-discover-repos: --depth may be given only once" --path "$VALID" --dry-run --depth 2 --depth 5
probe_rejected "git-discover-repos: --follow and --no-follow may not be combined" --path "$VALID" --dry-run --follow --no-follow
probe_rejected "git-discover-repos: --follow and --no-follow may not be combined" --path "$VALID" --dry-run --no-follow --follow
probe_rejected "may not be combined with" --path "$VALID" --dry-run --repos-only --worktrees-only
probe_rejected "may not be combined with" --path "$VALID" --dry-run --worktrees-only --repos-only
probe_rejected "git-discover-repos: --auto and --path may not be combined" --path "$VALID" --auto
probe_rejected "git-discover-repos: --auto and --path may not be combined" --auto --path "$VALID"
probe_rejected "git-discover-repos: --auto and --path may not be combined" "$VALID" --auto
probe_accepted --auto --dry-run
probe_accepted "$VALID" --dry-run
for bad in abc 0 2147483648 99999999999999999999999999; do
  probe_rejected "git-discover-repos: --depth must be a positive integer" --path "$VALID" --dry-run --depth "$bad"
done
probe_accepted --path "$VALID" --dry-run --depth 08

[ "$failures" -eq 0 ] || { echo "$failures failure(s)" >&2; exit 1; }
echo "all git-discover-repos --path exit-status checks passed"
