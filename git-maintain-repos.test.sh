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
mkdir -p "$VALID"
git init -q "$VALID/repo1"

run() { # run <args...> — sets OUT, ERR, STATUS
  OUT="$(bash "$SCRIPT" "$@" 2>"$TMP_ROOT/err")"; STATUS=$?
  ERR="$(cat "$TMP_ROOT/err")"
}

snapshot() { find "$VALID" "$HOME" | sort; }

# ─── git stub for the scheduler install step (#1402) ───────────────
# `git maintenance start` is platform machinery (launchd on macOS,
# systemd/cron elsewhere) and fails outright in some test environments
# (e.g. outside a repo, or with no user scheduler) -- an environment
# fact, not a driver fact. A stub on PATH controls just that one step
# for the whole suite: it succeeds unless GIT_STUB_MAINT_START_RC says
# otherwise, and delegates every other git call to the real git.
STUB_DIR="$TMP_ROOT/git-stub"
mkdir -p "$STUB_DIR"
REAL_GIT="$(command -v git)"
# The shebang must name a real interpreter: /usr/bin/env does not exist in
# the nix build sandbox (#1404), so the stub failed with "bad interpreter"
# and every git call -- including the reflog-expire parse probe -- took the
# stub down with it.
cat > "$STUB_DIR/git" <<EOF
#!$(command -v bash)
if [ "\${1:-}" = "maintenance" ] && [ "\${2:-}" = "start" ]; then
  if [ "\${GIT_STUB_MAINT_START_RC:-0}" != "0" ]; then
    echo "stub git: maintenance start failed" >&2
    exit "\$GIT_STUB_MAINT_START_RC"
  fi
  exit 0
fi
exec "$REAL_GIT" "\$@"
EOF
chmod +x "$STUB_DIR/git"
export PATH="$STUB_DIR:$PATH"

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

# ─── Per-repo registration happens in a real run (#1327) ─────────
# Usage and --dry-run always promised `git maintenance register` +
# gc.auto 0 + maintenance.auto false per repo, but the real run never
# did them. Registration is serial in the driver (one shared
# maintenance.config), so a real run over two repos under the default
# fan-out must register both, and --no-register must write nothing.
REG="$TMP_ROOT/reg"
for r in r1 r2; do
  mkdir -p "$REG/$r"
  git init -q "$REG/$r"
  git -C "$REG/$r" config user.email test@example.invalid
  git -C "$REG/$r" config user.name test
  echo x > "$REG/$r/file"
  git -C "$REG/$r" add file
  git -C "$REG/$r" commit -qm init
done
run --no-system --path "$REG" --quick
[ "$STATUS" -eq 0 ] || fail "registration run exited $STATUS (stderr: $ERR)"
MC="$HOME/.config/git/maintenance.config"
[ -f "$MC" ] || fail "registration run did not create maintenance.config"
registered="$(git config --file "$MC" --get-all maintenance.repo 2>/dev/null)"
for r in r1 r2; do
  case "$registered" in
    *"$REG/$r"*) echo "ok: $r registered in maintenance.config" ;;
    *) fail "$r not in maintenance.repo (got: $registered)" ;;
  esac
  [ "$(git -C "$REG/$r" config --get gc.auto 2>/dev/null)" = "0" ] \
    || fail "$r gc.auto not set to 0"
  [ "$(git -C "$REG/$r" config --get maintenance.auto 2>/dev/null)" = "false" ] \
    || fail "$r maintenance.auto not set to false"
done
echo "ok: registration run sets gc.auto/maintenance.auto in both repos"

# --no-register: no writes at all, in a fresh HOME where the config
# file's directory does not exist yet either.
OLD_HOME="$HOME"
export HOME="$TMP_ROOT/home-noreg"
mkdir -p "$HOME"
REGNR="$TMP_ROOT/reg-noreg"
mkdir -p "$REGNR/r1"
cp -R "$REG/r1/." "$REGNR/r1/"
git -C "$REGNR/r1" config --unset gc.auto
git -C "$REGNR/r1" config --unset maintenance.auto
run --no-system --path "$REGNR" --quick --no-register
[ "$STATUS" -eq 0 ] || fail "--no-register run exited $STATUS (stderr: $ERR)"
[ ! -e "$HOME/.config/git/maintenance.config" ] \
  || fail "--no-register created maintenance.config"
[ -z "$(git -C "$REGNR/r1" config --get gc.auto 2>/dev/null)" ] \
  || fail "--no-register set gc.auto"
[ -z "$(git -C "$REGNR/r1" config --get maintenance.auto 2>/dev/null)" ] \
  || fail "--no-register set maintenance.auto"
echo "ok: --no-register performs no registration writes"
export HOME="$OLD_HOME"

# A registration failure is named and changes the exit status: the
# maintenance.config path is a directory, so register cannot write it.
export HOME="$TMP_ROOT/home-regfail"
mkdir -p "$HOME/.config/git/maintenance.config"
run --no-system --path "$REG" --quick
[ "$STATUS" -ne 0 ] || fail "registration-failure run exited 0"
case "$ERR" in
  *"registration failed for $REG/r1"*) echo "ok: registration failure names the repo on stderr" ;;
  *) fail "registration failure did not name the repo on stderr (got: $ERR)" ;;
esac
export HOME="$OLD_HOME"

# ─── Per-repo maintenance failures reach the exit code (#1331) ────
# A corrupt blob makes gc fail at run time. On the old code the worker
# captured fsck_rc only to gate orphan cleanup and ended `exit 0`, the
# gc pipe never captured PIPESTATUS, the quick tasks discarded
# stderr+status, and the driver never checked the xargs fan-out -- a
# corrupt repo reported a clean run at exit 0.
FAILROOT="$TMP_ROOT/failrepos"
for r in good bad; do
  mkdir -p "$FAILROOT/$r"
  git init -q "$FAILROOT/$r"
  git -C "$FAILROOT/$r" config user.email test@example.invalid
  git -C "$FAILROOT/$r" config user.name test
  echo x > "$FAILROOT/$r/file"
  git -C "$FAILROOT/$r" add file
  git -C "$FAILROOT/$r" commit -qm init
done
blob="$(git -C "$FAILROOT/bad" rev-parse HEAD:file)"
obj="$FAILROOT/bad/.git/objects/${blob:0:2}/${blob:2}"
chmod +w "$obj"
printf garbage > "$obj"

# Worker directly, deep pass: gc on the corrupt repo fails, so the
# worker must exit non-zero and name the repo + the failed step.
WERR="$TMP_ROOT/worker.err"
GMR_ONE=1 DEPTH=deep FSCK=1 bash "$SCRIPT" --one "$FAILROOT/bad" >"$TMP_ROOT/worker.out" 2>"$WERR"; WSTATUS=$?
[ "$WSTATUS" -ne 0 ] || fail "worker (deep) on corrupt repo exited 0"
case "$(cat "$WERR")" in
  *"maintenance failed for $FAILROOT/bad: git gc"*) echo "ok: worker names repo + gc step on stderr and exits non-zero" ;;
  *) fail "worker stderr did not name repo+gc step (got: $(cat "$WERR"))" ;;
esac

# Worker on the healthy control still exits 0.
GMR_ONE=1 DEPTH=quick FSCK=1 bash "$SCRIPT" --one "$FAILROOT/good" >/dev/null 2>&1; WSTATUS=$?
[ "$WSTATUS" -eq 0 ] || fail "worker on healthy repo exited $WSTATUS (want 0)"
echo "ok: worker on healthy repo exits 0"

# Driver over both repos: the healthy repo is still processed, the
# exit is non-zero, and the summary names the failed repo.
run --no-system --path "$FAILROOT" --deep --no-register
[ "$STATUS" -ne 0 ] || fail "driver on corrupt repo exited 0"
case "$ERR" in
  *"maintenance failed for 1 of 2 repo(s)"*"$FAILROOT/bad"*) echo "ok: driver aggregates the worker failure, names the repo, exits non-zero" ;;
  *) fail "driver did not summarise the failed repo (stderr: $ERR)" ;;
esac
case "$OUT" in
  *"good"*) echo "ok: driver still processed the healthy repo" ;;
  *) fail "driver output did not include the healthy repo (got: $OUT)" ;;
esac

# Driver on the healthy repo alone is unchanged: exit 0.
run --no-system --path "$FAILROOT/good" --quick --no-register
[ "$STATUS" -eq 0 ] || fail "driver on healthy repo exited $STATUS (stderr: $ERR)"
echo "ok: driver on healthy repo exits 0"

# ─── Scheduler install failure is named and changes the exit (#1402) ──
# ensure_scheduler used to run `git maintenance start 2>/dev/null` and
# return 0 unconditionally, so a failed scheduler install looked exactly
# like success in both --system and full scopes. A git stub on PATH
# controls that one step: every other git call delegates to the real git.
# Uses the git stub installed near the top of this suite (see #1402
# stub block): GIT_STUB_MAINT_START_RC controls the install step.
export HOME="$TMP_ROOT/home-sched"
mkdir -p "$HOME"

# Failing stub, --system: git's own error is not discarded, the step is
# named on stderr, and the run exits non-zero.
export GIT_STUB_MAINT_START_RC=128
run --system
[ "$STATUS" -ne 0 ] || fail "scheduler-failure --system run exited 0"
case "$ERR" in
  *"stub git: maintenance start failed"*"scheduler install failed: git maintenance start"*) echo "ok: failed maintenance start named on stderr, git error preserved" ;;
  *) fail "scheduler failure did not name the step on stderr (got: $ERR)" ;;
esac
case "$OUT" in
  *"scheduler:"*) echo "ok: scheduler report still printed on the failure path" ;;
  *) fail "scheduler failure run did not print the scheduler report (got: $OUT)" ;;
esac

# Succeeding stub, --system: unchanged control output, exit 0.
export GIT_STUB_MAINT_START_RC=0
run --system
[ "$STATUS" -eq 0 ] || fail "scheduler-success --system run exited $STATUS (stderr: $ERR)"
case "$OUT" in
  *"scheduler: 0 launchd agents, 0 repos registered"*) echo "ok: successful maintenance start keeps the unchanged scheduler line, exits 0" ;;
  *) fail "scheduler success run changed the scheduler line (got: $OUT)" ;;
esac

# Full scope: a failing scheduler install drives a non-zero exit even
# when registration and per-repo maintenance over the fixture succeed.
export HOME="$TMP_ROOT/home"
export GIT_STUB_MAINT_START_RC=128
run --path "$REG" --quick
[ "$STATUS" -ne 0 ] || fail "full-scope run with failing maintenance start exited 0"
case "$ERR" in
  *"scheduler install failed: git maintenance start"*) echo "ok: full-scope scheduler failure named and exits non-zero" ;;
  *) fail "full-scope scheduler failure did not name the step on stderr (got: $ERR)" ;;
esac
export GIT_STUB_MAINT_START_RC=0
run --path "$REG" --quick
[ "$STATUS" -eq 0 ] || fail "full-scope control run exited $STATUS (stderr: $ERR)"
echo "ok: full-scope control run exits 0"
unset GIT_STUB_MAINT_START_RC

[ "$failures" -eq 0 ] || { echo "$failures failure(s)" >&2; exit 1; }
echo "all git-maintain-repos flag-shaped-value checks passed"
