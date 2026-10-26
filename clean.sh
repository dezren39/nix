#!/usr/bin/env bash
# SPDX-License-Identifier: MIT OR Apache-2.0
#
# clean.sh — reclaim disk: caches, logs, build artifacts, then git maintenance.
#
# STRUCTURE
#   1. system/user caches (brew, nix, docker, package managers, macOS)
#   2. logs
#   3. per-repo work: .trash, build artifacts, Spotlight markers
#   4. git maintenance, last, because it is the slowest part
#
# Discovery for step 3/4 is shared with git-maintain-repos and
# spotlight-exclude-artifacts via ./git-discover-repos, and the directory-name
# lists come from lib/common.sh. None of the three keeps its own copy.

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ---------------------------------------------------------------------------
# Flags. Vocabulary matches git-maintain-repos and spotlight-exclude-artifacts.
# ---------------------------------------------------------------------------
SCOPE="auto"          # auto | system | no-system
AUTO_SET=0
SCOPE_SET=0
TARGET_PATH=""
DEPTH_LIMIT=""
RECURSIVE_SET=0
FOLLOW=""
DRY_RUN=0
DO_MAINTENANCE=1
MAINT_DEPTH="--deep"
REPO_FLAGS_USED=""
USE_SUDO=""          # empty = decide from --dry-run
CLEAN_TMPDIR="${CLEAN_TMPDIR:-0}"   # opt-in: wipe /private/var/folders
EXPIRE_REFLOG="${EXPIRE_REFLOG:-0}" # opt-in: force reflog expiry during maintenance
DOCKER_AGE_DAYS="${DOCKER_AGE_DAYS:-}"  # set = age-scoped docker prune, VM preserved
PRUNE_UNKNOWN_SCRATCH="${PRUNE_UNKNOWN_SCRATCH:-0}" # opt-in: delete, not just flag, the name-blind catch-all
UNKNOWN_SCRATCH_MIN_MB="${UNKNOWN_SCRATCH_MIN_MB:-500}"
UNKNOWN_SCRATCH_STALE_DAYS="${UNKNOWN_SCRATCH_STALE_DAYS:-7}"
# --only-unknown-scratch: skip everything else in the script and just run
# that one sweep against both scratch roots, then exit. For trying it (with
# or without --prune-unknown-scratch) without paying for docker/brew/git
# maintenance/etc. first.
ONLY_UNKNOWN_SCRATCH="${ONLY_UNKNOWN_SCRATCH:-0}"
# Collects every name-blind catch-all hit (flagged or, with
# --prune-unknown-scratch, deleted) for the report printed at the very end.
# A plain array, not a file: this runs once per invocation, in one process,
# no subshell boundary separates the sweep from the final report.
UNKNOWN_SCRATCH_HITS=()

usage() {
  cat <<'EOF'
clean.sh — reclaim disk space

USAGE
  ./clean                          everything (default; same as --auto)
  ./clean --path DIR               per-repo work for DIR only
  ./clean --system                 caches/logs only, touch no repositories
  ./clean --no-system              repositories only, skip caches/logs
  ./clean --dry-run                report, change nothing

SCOPE (mutually exclusive; --auto names the default)
  --auto            Caches + logs + repositories. Passing it with --system or
                    --no-system is an error.
  --system          Caches, logs, package managers, colima. No repositories.
  --no-system       Repository half only: .trash, artifacts, markers, git
                    maintenance across the full discovered set.

DISCOVERY (shared with git-maintain-repos / spotlight-exclude-artifacts)
  --path DIR        Restrict repository work to DIR. Skips the cache/log half
                    and the global maintenance --system pass. Implies
                    --no-follow.
  --recursive       Descend without limit. Default.
  --no-recursive    Equivalent to --depth 1 (path + direct children).
  --depth N         Explicit find -maxdepth.
  --follow          Hop worktree -> real repo -> its other worktrees.
                    Default under --auto.
  --no-follow       Stay put. Default under --path.

MAINTENANCE
  --no-git-maintenance
                    Skip the git-maintain-repos pass entirely, leaving clean.sh
                    doing exactly what it did before maintenance was wired in.
                    Synonyms: --no-maintenance, --no-git.
  --quick           Run maintenance at --quick instead of --deep.
  --dry-run         Print what would happen; deletes nothing.

OPT-IN DESTRUCTION (both off by default; measured as not worth their risk)
  --tmpdir          Also wipe /private/var/folders, the live per-user temp
                    directory. ~350 MB here, and it takes $TMPDIR out from
                    under processes that are running right now. The OS sweeps
                    this path on its own anyway.
  --docker-age N    Age-scoped docker cleanup: remove containers, images and
                    dangling volumes unused for more than N days, and never
                    touch the colima VM itself. Without it, docker cleanup is
                    all-or-nothing (`system prune --all --volumes`) and the VM
                    may be recreated when it looks empty.
  --expire-reflog   Force `git reflog expire` during --deep maintenance.
                    Measured 0 MB of reclaim across the largest repos here:
                    almost nothing is held by reflog alone. Without it, gc
                    still expires at git's defaults (90d / 30d unreachable).
  --opencode-root-scratch
                    Also sweep ~/.local/share/opencode itself for stray
                    one-off dirs sessions sometimes drop there directly
                    (gated the same as tmp/opencode: idle age +
                    clean/pushed). Off by default: the protected-name list
                    this checks against can go stale as opencode adds new
                    top-level files, so this is opt-in and worth a --dry-run
                    first.
  --prune-unknown-scratch
                    Without this flag, large+old scratch entries that match
                    NO known name pattern (generic mktemp output, random
                    throwaway dirs) are only FLAGGED -- reported in a list at
                    the end, never deleted, because "looks generic and old"
                    is weaker evidence than a known tool's own naming
                    convention. Pass this to actually delete what the flagged
                    report would have listed (still gated on age, size, and
                    no open handle -- see UNKNOWN_SCRATCH_* below).
  --only-unknown-scratch
                    Run JUST the name-blind catch-all above, against both
                    scratch roots, then print the report and exit --
                    everything else in this script (docker, brew, uv/bun,
                    git maintenance, the named-pattern sweep, all of it) is
                    skipped. For trying the catch-all on its own, cheaply:
                    no sudo prompt (nothing it touches is root-owned),
                    combine with --dry-run or --prune-unknown-scratch the
                    same as a full run.

TUNABLES (environment)
  (validated at startup: a non-integer value — or 0 where 0 is not
  meaningful — is rejected with exit 2 before anything runs)
  ARTIFACT_STALE_DAYS       default 1   repo/worktree idle period before artifacts
                                        are removed (paired with a clean-tree check)
  HOME_ARTIFACT_STALE_DAYS  default 7   same, for artifact dirs directly in ~, where
                                        no repository gate applies
  COLIMA_STOPPED_STALE_DAYS default 10  a STOPPED, non-default colima profile
                                        whose disk file hasn't been written to in
                                        this long is deleted on mtime alone (no
                                        live daemon to verify emptiness); `default`
                                        is never considered regardless of age
  OPENCODE_TMP_STALE_DAYS       default 7   age gate for loose entries in
                                        ~/.local/share/opencode/tmp/opencode,
                                        git-gated like HOME_ARTIFACT_STALE_DAYS
  OPENCODE_WORKTREE_STALE_DAYS  default 7   same, for
                                        ~/.local/share/opencode/worktree
  OPENCODE_ROOT_STALE_DAYS      default 7   same, for stray entries dropped
                                        directly in ~/.local/share/opencode
                                        (app's own files/dirs are protected;
                                        requires --opencode-root-scratch)
  OPENCODE_TIDY_PROTECT_DAYS    default 1   tmp/opencode tidy pass: a dir with
                                        any file modified within this window
                                        (anywhere in its subtree) is entirely
                                        skipped, never partially reorganised
  OPENCODE_TIDY_LARGE_MB        default 200 tmp/opencode tidy pass: postgres
                                        data dirs (by PG_VERSION marker) and
                                        anything at or above this size inside
                                        an eligible leaf dir are deleted
                                        outright before the remainder is
                                        archived into _loose/<year>/<month>/
  CACHE_STALE_DAYS     default 3   age gate for nix caches / flake GC roots
  LOG_STALE_DAYS       default 0   0 = drop all logs, N = keep last N days
  SHORT_SCRATCH_STALE_HOURS default 8   age gate (hours) for tool-prefixed
                                        single-run fixture dirs in
                                        ~/.local/share/opencode/tmp AND
                                        /private/var/folders/*/*/T (always
                                        on, not behind --tmpdir)
  PG_SCRATCH_STALE_HOURS    default 8   age gate (hours) for ad hoc postgres
                                        data dirs (by PG_VERSION marker),
                                        found at the top of either scratch
                                        root above OR inside any discovered
                                        repo/worktree; paired with an lsof
                                        open-handle check either way, so a
                                        running cluster stays safe regardless
  ACTIVE_ARTIFACT_STALE_HOURS default 8 age gate (hours) for a cache artifact
                                        (.venv, node_modules, .cache, ...)
                                        inside a repo/worktree whose SOURCE
                                        is dirty or recently touched -- the
                                        repo being active no longer protects
                                        its build caches once they're this
                                        stale and nothing has them open
  UNKNOWN_SCRATCH_MIN_MB   default 500  size floor (MB) for the name-blind
                                        catch-all in the two scratch roots --
                                        below this, "generic and old" isn't
                                        worth a report entry
  UNKNOWN_SCRATCH_STALE_DAYS default 7  age floor (days, own mtime + no open
                                        handle) for that same catch-all;
                                        matches the worktree-removal window,
                                        not the 8h tool-fixture windows above,
                                        since a generic name carries no tool
                                        lifecycle evidence to lean on
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --auto)           AUTO_SET=1 ;;
    --system)         SCOPE="system"; SCOPE_SET=1 ;;
    --no-system)      SCOPE="no-system"; SCOPE_SET=1 ;;
    --path)           TARGET_PATH="${2:-}"; REPO_FLAGS_USED="$REPO_FLAGS_USED --path"; shift ;;
    --recursive|--walk) REPO_FLAGS_USED="$REPO_FLAGS_USED --recursive"; DEPTH_LIMIT=""; RECURSIVE_SET=1 ;;
    --no-recursive|--no-walk) DEPTH_LIMIT=1; RECURSIVE_SET=1 ;;
    --depth)          DEPTH_LIMIT="${2:-1}"; RECURSIVE_SET=1; REPO_FLAGS_USED="$REPO_FLAGS_USED --depth"; shift ;;
    --follow)         FOLLOW=1; REPO_FLAGS_USED="$REPO_FLAGS_USED --follow" ;;
    --no-follow)      FOLLOW=0; REPO_FLAGS_USED="$REPO_FLAGS_USED --no-follow" ;;
    --sudo)           USE_SUDO=1 ;;
    --tmpdir)         CLEAN_TMPDIR=1 ;;
    --expire-reflog)  EXPIRE_REFLOG=1 ;;
    --docker-age)     DOCKER_AGE_DAYS="${2:-30}"; shift ;;
    --opencode-root-scratch) CLEAN_OPENCODE_ROOT=1 ;;
    --prune-unknown-scratch) PRUNE_UNKNOWN_SCRATCH=1 ;;
    --only-unknown-scratch) ONLY_UNKNOWN_SCRATCH=1 ;;
    --no-sudo)        USE_SUDO=0 ;;
    --no-git-maintenance|--no-maintenance|--no-git) DO_MAINTENANCE=0 ;;
    --quick)          MAINT_DEPTH="--quick" ;;
    --dry-run)        DRY_RUN=1 ;;
    -h|--help)        usage; exit 0 ;;
    -*)               echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
    *)                TARGET_PATH="$1" ;;
  esac
  shift
done

# --depth is forwarded verbatim to find -maxdepth (via git-discover-repos),
# where a non-numeric value fails mid-walk and 0 silently means "the path
# itself only". Judge it whole, at parse time, like opencode-db-prune.sh
# judges --protect-days/--batch (#830). Empty means unbounded (--recursive).
# This block is identical in all four discovery-vocabulary scripts.
if [ -n "$DEPTH_LIMIT" ]; then
  case "$DEPTH_LIMIT" in
    *[!0-9]*|'')
      echo "clean.sh: --depth must be a positive integer (got: $DEPTH_LIMIT)" >&2
      exit 2 ;;
  esac
  # Range backstop (#858): find parses -maxdepth as a C int, so a digits-only
  # value above 2147483647 passes the case above yet find rejects it at walk
  # time ("Numerical result out of range") while the run still exits 0. The
  # shell's own integer tests cannot judge this — they are 64-bit — so, like
  # the --pack-threads backstop (#839/#840), compare by digit count first
  # (leading zeros stripped; at most 10 digits always fits the shell's
  # arithmetic), then by value against INT_MAX. Judging the stripped value
  # here, before the -gt 0 test below, also rejects >64-bit digit strings
  # cleanly instead of tripping "integer expression expected".
  _dl=${DEPTH_LIMIT#"${DEPTH_LIMIT%%[!0]*}"}; _dl=${_dl:-0}
  if [ "${#_dl}" -gt 10 ] || [ "$_dl" -gt 2147483647 ]; then
    echo "clean.sh: --depth must be a positive integer (got: $DEPTH_LIMIT)" >&2
    exit 2
  fi
  unset _dl
  [ "$DEPTH_LIMIT" -gt 0 ] || { echo "clean.sh: --depth must be a positive integer (got: $DEPTH_LIMIT)" >&2; exit 2; }
fi

# --docker-age is used only in bash arithmetic ($((DOCKER_AGE_DAYS * 24)) and
# the volume cutoff), where a bare word silently evaluates to 0 -- so
# `--docker-age abc` used to collapse the age filter to until=0h and prune
# EVERYTHING, and junk like 10junk/1.5 aborted mid-run with a raw arithmetic
# error (#829). Judge it whole, at parse time, like opencode-db-prune.sh
# judges --protect-days/--batch. Covers the DOCKER_AGE_DAYS env var too.
if [ -n "$DOCKER_AGE_DAYS" ]; then
  case "$DOCKER_AGE_DAYS" in
    *[!0-9]*|'')
      echo "clean.sh: --docker-age must be a positive integer (got: $DOCKER_AGE_DAYS)" >&2
      exit 2 ;;
  esac
  # Range backstop (#862): the use sites multiply this value (* 24, * 86400)
  # in bash's 64-bit arithmetic, so a digits-only value that merely fits
  # 64-bit can still wrap at the multiplication (the * 86400 products wrap
  # negative from ~1.07e14, inverting the stale cutoff to the far future).
  # Judge the stripped value by digit count first, then by value against
  # INT_MAX, before the -gt 0 test below — the #840/#858 technique. INT_MAX
  # is wrap-proof here: the largest multiplier in this file is 86400 and
  # 2147483647 * 86400 ~ 1.9e14 fits 64-bit easily.
  _da=${DOCKER_AGE_DAYS#"${DOCKER_AGE_DAYS%%[!0]*}"}; _da=${_da:-0}
  if [ "${#_da}" -gt 10 ] || [ "$_da" -gt 2147483647 ]; then
    echo "clean.sh: --docker-age must be a positive integer (got: $DOCKER_AGE_DAYS)" >&2
    exit 2
  fi
  unset _da
  [ "$DOCKER_AGE_DAYS" -gt 0 ] || { echo "clean.sh: --docker-age must be a positive integer (got: $DOCKER_AGE_DAYS)" >&2; exit 2; }
fi

# The numeric env tunables (see TUNABLES in usage) gate deletion — age,
# idle and size floors — and reach three kinds of use site, each of which
# fails differently on a junk value (#837): bash arithmetic silently
# evaluates a bare word to 0 (the age gate vanishes), a find -mtime/-mmin
# argument errors while the idle checks discard stderr and read the empty
# result as "nothing modified recently" (the idle gate inverts), and an
# integer test errors into the wrong branch. #829 judged DOCKER_AGE_DAYS
# alone; judge every remaining tunable the same way, here at parse time,
# before any sweep runs and before the sudo re-exec below forwards the
# values via --preserve-env. Most must be positive; 0 is meaningful only
# for LOG_STALE_DAYS (documented "0 = drop all logs"), so it is judged
# non-negative. Unset or empty is fine and skips the check: the later
# ${VAR:-default} assignments supply a known-good literal, and an empty
# env value takes that default the same way.
_clean_positive_vars="ARTIFACT_STALE_DAYS HOME_ARTIFACT_STALE_DAYS CACHE_STALE_DAYS COLIMA_STOPPED_STALE_DAYS OPENCODE_TMP_STALE_DAYS OPENCODE_WORKTREE_STALE_DAYS OPENCODE_ROOT_STALE_DAYS OPENCODE_TIDY_PROTECT_DAYS OPENCODE_TIDY_LARGE_MB SHORT_SCRATCH_STALE_HOURS PG_SCRATCH_STALE_HOURS ACTIVE_ARTIFACT_STALE_HOURS PYTEST_KEEP_HOURS UNKNOWN_SCRATCH_MIN_MB UNKNOWN_SCRATCH_STALE_DAYS NIX_KEEP_DAYS NIX_KEEP_GENS"
for _v in $_clean_positive_vars; do
  _val="${!_v:-}"
  [ -n "$_val" ] || continue
  case "$_val" in
    *[!0-9]*|'')
      echo "clean.sh: $_v must be a positive integer (got: $_val)" >&2
      exit 2 ;;
  esac
  # Range backstop (#862), same reasoning as --docker-age above: these
  # values are multiplied at their use sites (* 86400, * 1440, * 60), so
  # cap them at INT_MAX with the strip/digit-count/value technique before
  # the -gt 0 test, which also rejects >64-bit digit strings cleanly.
  _sv=${_val#"${_val%%[!0]*}"}; _sv=${_sv:-0}
  if [ "${#_sv}" -gt 10 ] || [ "$_sv" -gt 2147483647 ]; then
    echo "clean.sh: $_v must be a positive integer (got: $_val)" >&2
    exit 2
  fi
  [ "$_val" -gt 0 ] || { echo "clean.sh: $_v must be a positive integer (got: $_val)" >&2; exit 2; }
done
unset _clean_positive_vars _v _val _sv
if [ -n "${LOG_STALE_DAYS:-}" ]; then
  case "$LOG_STALE_DAYS" in
    *[!0-9]*|'')
      echo "clean.sh: LOG_STALE_DAYS must be a non-negative integer (got: $LOG_STALE_DAYS)" >&2
      exit 2 ;;
  esac
  # Range backstop (#862): the value is forwarded to find -mtime, a C-int
  # consumer, so cap it at INT_MAX with the same strip/digit-count/value
  # technique as the positive tunables above.
  _sv=${LOG_STALE_DAYS#"${LOG_STALE_DAYS%%[!0]*}"}; _sv=${_sv:-0}
  if [ "${#_sv}" -gt 10 ] || [ "$_sv" -gt 2147483647 ]; then
    echo "clean.sh: LOG_STALE_DAYS must be a non-negative integer (got: $LOG_STALE_DAYS)" >&2
    exit 2
  fi
  unset _sv
fi

# --system touches no repositories, so repository-scoped flags are meaningless
# alongside it. Erroring beats silently ignoring them.
if [ "$SCOPE" = "system" ] && [ -n "$REPO_FLAGS_USED" ]; then
  echo "clean.sh: --system conflicts with repository-scoped flags:$REPO_FLAGS_USED" >&2
  echo "          (--system does caches/logs only and opens no repository)" >&2
  exit 2
fi

# --dry-run writes nothing, so it should not demand a password. --sudo forces
# escalation anyway; combined with --dry-run that is a no-op and says so.
# --only-unknown-scratch never touches a root-owned path either (both
# scratch roots are entirely user-owned), so it gets the same default.
if [ -z "$USE_SUDO" ]; then
  if [ "$DRY_RUN" = "1" ] || [ "${ONLY_UNKNOWN_SCRATCH:-0}" = "1" ]; then USE_SUDO=0; else USE_SUDO=1; fi
elif [ "$USE_SUDO" = "1" ] && [ "$DRY_RUN" = "1" ]; then
  echo "clean.sh: --sudo with --dry-run is a no-op (nothing is written either way)" >&2
fi

if [ "$AUTO_SET" = "1" ] && [ "$SCOPE_SET" = "1" ]; then
  echo "clean.sh: --auto conflicts with --system/--no-system" >&2
  echo "          (--auto just names the default: caches + logs + repositories)" >&2
  exit 2
fi
# --path is inherently repository-scoped.
[ -n "$TARGET_PATH" ] && [ "$SCOPE" = "auto" ] && SCOPE="no-system"

DO_CACHES=1; DO_REPOS=1
case "$SCOPE" in
  system)    DO_REPOS=0 ;;
  no-system) DO_CACHES=0 ;;
esac

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
C_WARN=$'\033[1;33m'; C_INFO=$'\033[0;36m'; C_OFF=$'\033[0m'
warn() { printf '%s⚠️  %s%s\n' "$C_WARN" "$*" "$C_OFF" >&2; }
info() { printf '%sℹ️  %s%s\n' "$C_INFO" "$*" "$C_OFF"; }

get_free_bytes() { df -k / | awk 'NR==2 {print $4 * 1024}'; }

# On Apple Silicon the real prefix is /opt/homebrew. A stale /usr/local/bin/brew
# can shadow it on PATH -- it did here, so `brew cleanup` was running against an
# almost-empty prefix (1 cask) while the actual install had 71. Always resolve
# the prefix with the most casks rather than trusting PATH.
real_brew() {
  local best="" bestn=-1 p n
  for p in /opt/homebrew/bin/brew /usr/local/bin/brew "$(command -v brew 2>/dev/null)"; do
    [ -x "$p" ] || continue
    n=$(ls "$("$p" --prefix 2>/dev/null)/Caskroom" 2>/dev/null | wc -l | tr -d ' ')
    [ "${n:-0}" -gt "$bestn" ] && { bestn="${n:-0}"; best="$p"; }
  done
  printf '%s' "$best"
}
brew_cleanup() {
  local b; b="$(real_brew)"
  if [ -z "$b" ]; then info "brew not installed, skipping"; return 0; fi
  echo "🍺 brew cleanup ($b, prefix $("$b" --prefix 2>/dev/null))"
  "$b" cleanup --prune=all || warn "brew cleanup failed"
  # Casks whose app bundle is gone leave Caskroom metadata and broken symlinks.
  "$b" cleanup --prune=all --quiet 2>/dev/null || true
}

human_size() {
  local bytes=$1
  if (( bytes >= 1073741824 )); then printf "%.2f GB" "$(echo "scale=2; $bytes / 1073741824" | bc)"
  elif (( bytes >= 1048576 )); then printf "%.2f MB" "$(echo "scale=2; $bytes / 1048576" | bc)"
  else printf "%d KB" "$(( bytes / 1024 ))"; fi
}

# Every destructive action funnels through these so --dry-run is honoured
# uniformly and a missing tool warns instead of aborting the run.
run() {
  if [ "$DRY_RUN" = "1" ]; then echo "  would run: $*"; return 0; fi
  "$@"
}
del() {
  local p
  for p in "$@"; do
    [ -e "$p" ] || continue
    if [ "$DRY_RUN" = "1" ]; then echo "  would delete: ${p/#$HOME/\~}"; continue; fi
    chmod -R u+w "$p" 2>/dev/null
    rm -rf "$p" 2>/dev/null || warn "could not fully remove $p"
  done
}
# have CMD -- true if runnable. Missing tools are informational, never fatal.
have() { command -v "$1" >/dev/null 2>&1; }

# ---------------------------------------------------------------------------
# Privilege handling
# ---------------------------------------------------------------------------
if [ "$EUID" -ne 0 ]; then
  DISK_FREE_START="$(get_free_bytes)"
  echo "📏 Free disk before clean: $(human_size "$DISK_FREE_START")"
  # Root-only paths simply report as inaccessible when unprivileged.
  if [ "$USE_SUDO" = "0" ]; then
    info "no-sudo: staying unprivileged; root-owned paths will be skipped"
    export CALLER_USER_HOME="$(realpath ~)"
  else
    if [ "$DO_CACHES" = "1" ]; then brew_cleanup; fi
    CALLER_USER_HOME="$(realpath ~)" DISK_FREE_START="$DISK_FREE_START" \
      ARTIFACT_STALE_DAYS="${ARTIFACT_STALE_DAYS:-1}" LOG_STALE_DAYS="${LOG_STALE_DAYS:-0}" \
      CACHE_STALE_DAYS="${CACHE_STALE_DAYS:-3}" \
      HOME_ARTIFACT_STALE_DAYS="${HOME_ARTIFACT_STALE_DAYS:-7}" \
      COLIMA_STOPPED_STALE_DAYS="${COLIMA_STOPPED_STALE_DAYS:-10}" \
      OPENCODE_TMP_STALE_DAYS="${OPENCODE_TMP_STALE_DAYS:-7}" \
      OPENCODE_WORKTREE_STALE_DAYS="${OPENCODE_WORKTREE_STALE_DAYS:-7}" \
      OPENCODE_ROOT_STALE_DAYS="${OPENCODE_ROOT_STALE_DAYS:-7}" \
      OPENCODE_TIDY_PROTECT_DAYS="${OPENCODE_TIDY_PROTECT_DAYS:-1}" \
      OPENCODE_TIDY_LARGE_MB="${OPENCODE_TIDY_LARGE_MB:-200}" \
      CLEAN_TMPDIR="$CLEAN_TMPDIR" EXPIRE_REFLOG="$EXPIRE_REFLOG" DOCKER_AGE_DAYS="$DOCKER_AGE_DAYS" \
      CLEAN_OPENCODE_ROOT="$CLEAN_OPENCODE_ROOT" \
      sudo --preserve-env=CALLER_USER_HOME,DISK_FREE_START,ARTIFACT_STALE_DAYS,LOG_STALE_DAYS,CACHE_STALE_DAYS,HOME_ARTIFACT_STALE_DAYS,COLIMA_STOPPED_STALE_DAYS,OPENCODE_TMP_STALE_DAYS,OPENCODE_WORKTREE_STALE_DAYS,OPENCODE_ROOT_STALE_DAYS,OPENCODE_TIDY_PROTECT_DAYS,OPENCODE_TIDY_LARGE_MB,CLEAN_TMPDIR,EXPIRE_REFLOG,DOCKER_AGE_DAYS,CLEAN_OPENCODE_ROOT \
        "$0" "$@"
    exit $?
  fi
fi

ROOT_HOME="$(realpath ~)"
if [ -z "${CALLER_USER_HOME:-}" ]; then
  if [ -n "${SUDO_USER:-}" ]; then CALLER_USER_HOME="$(eval echo "~$SUDO_USER")"
  else CALLER_USER_HOME="$ROOT_HOME"; fi
fi
CALLER_USER="${SUDO_USER:-$(basename "$CALLER_USER_HOME")}"

# Anything that CREATES files must run as the caller. A previous root run left
# ~330 MB of root-owned files in ~/.cache that the user could not delete.
as_user() { sudo -u "$CALLER_USER" env HOME="$CALLER_USER_HOME" "$@"; }
as_user_have() { sudo -u "$CALLER_USER" env HOME="$CALLER_USER_HOME" sh -c "command -v $1 >/dev/null 2>&1"; }

# 1 day, not 7: the clean-working-tree gate in repo_is_idle now carries the
# "someone is mid-task" signal, so mtime only has to catch a tree edited very
# recently. Artifacts pruned here are all regenerable and all gitignored.
ARTIFACT_STALE_DAYS="${ARTIFACT_STALE_DAYS:-1}"
# Scope-independent (not just inside DO_CACHES): the per-repo postgres-dir
# sweep in HALF 2 needs this too, and --no-system runs HALF 2 without ever
# entering HALF 1. Left unset there, the arithmetic below would silently
# evaluate to 0 -- `-mmin -0` matches nothing, which reads as "stale" and
# would prune every postgres dir found regardless of real activity.
PG_SCRATCH_STALE_HOURS="${PG_SCRATCH_STALE_HOURS:-8}"
# Artifact dirs sitting directly in ~ are NOT inside any repo, so no git gate
# applies and mtime is the only evidence available. Keeps the old 7 days.
HOME_ARTIFACT_STALE_DAYS="${HOME_ARTIFACT_STALE_DAYS:-7}"
CACHE_STALE_DAYS="${CACHE_STALE_DAYS:-3}"
LOG_STALE_DAYS="${LOG_STALE_DAYS:-0}"
# A stopped, non-default colima VM whose disk has not been written to in this
# long is reclaimed on mtime alone -- see the colima block below for why that
# is sound even without a live daemon to ask.
COLIMA_STOPPED_STALE_DAYS="${COLIMA_STOPPED_STALE_DAYS:-10}"

# shellcheck source=lib/common.sh
. "$SELF_DIR/lib/common.sh"
build_name_expr CACHE_EXPR "${CACHE_NAMES[@]}"
build_name_expr RISK_EXPR  "${RISKY_NAMES[@]}"
build_name_expr TRASH_EXPR "${TRASH_NAMES[@]}"

# Name-blind-vs-known-pattern scratch matching. Defined here (scope-
# independent, before either HALF runs) rather than inline in HALF 1,
# specifically so --only-unknown-scratch can call sweep_unknown_large_scratch
# without running docker/brew/uv/the named-pattern sweep/anything else first.
SHORT_SCRATCH_GLOBS=(
  'mcpx-fake-*' 'mcpx-inline-*' 'mcpx-task-*' 'mcpx-run-*' 'mcpx-keep-*' 'mcpx-bin-*'
  'mw_ctrl_test_*' 'telemetry_test_*' 'telemetry_init_*'
  'playwright-artifacts-*' 'playwright_chromiumdev_profile-*' 'puppeteer_dev_chrome_profile-*'
  'chrome-*-profile' 'sbm-*' 'openchamber-*'
  'e2e_*' 'integration_*' 'opencode-*' 'v8-compile-cache-*'
  'go-build*' 'node-compile-cache' 'bunx-*'
  'nix-shell.*' 'nix-develop-*'
)

# `keep` in the name is a deliberate do-not-touch signal (e.g. a one-off
# someone named mcpx-keep-run to hold onto on purpose), EXCEPT
# mcpx-keep-<digits> itself: despite the name, that is the same disposable
# fixture as mcpx-fake/-task/-run under a different label.
short_scratch_is_kept() {
  case "$1" in
    mcpx-keep-[0-9]*) return 1 ;; # not actually protected
    *keep*) return 0 ;;           # protected
    *) return 1 ;;
  esac
}

# Name-blind catch-all: generic mktemp output (tmp.XXXXXXXX), random
# throwaway dirs (oc-XXXXXX), anything SHORT_SCRATCH_GLOBS above doesn't
# recognize. FLAGGED ONLY by default -- a known tool's own naming convention
# is real evidence an entry is disposable; "big and old" alone is weaker
# evidence, so this reports instead of deleting unless
# --prune-unknown-scratch says otherwise. Gated on size
# (UNKNOWN_SCRATCH_MIN_MB) + age (UNKNOWN_SCRATCH_STALE_DAYS -- deliberately
# the slower, days-based window shared with whole-worktree removal, not the
# hours-based windows above: a nameless entry carries no tool-lifecycle
# evidence to justify a short fuse) + no open handle, same discipline as
# every other sweep in this file. Every hit, flagged or deleted, is recorded
# for the report printed at the very end.
unknown_scratch_is_known() {
  local name="$1" g
  short_scratch_is_kept "$name" && return 0   # mcpx-keep-style: not ours to judge
  for g in "${SHORT_SCRATCH_GLOBS[@]}"; do
    # shellcheck disable=SC2053
    [[ "$name" == $g ]] && return 0
  done
  return 1
}
sweep_unknown_large_scratch() {
  local base="$1" entry name size_kb size_mb age_cutoff_min created modified status
  [ -d "$base" ] || return 0
  age_cutoff_min=$((UNKNOWN_SCRATCH_STALE_DAYS * 1440))
  for entry in "$base"/*; do
    [ -e "$entry" ] || continue
    name="${entry##*/}"
    [ "$name" = "opencode" ] && continue   # owned by the git-aware tidy pass, not this
    unknown_scratch_is_known "$name" && continue
    size_kb=$(du -sk "$entry" 2>/dev/null | cut -f1); size_kb=${size_kb:-0}
    size_mb=$((size_kb / 1024))
    [ "$size_mb" -ge "$UNKNOWN_SCRATCH_MIN_MB" ] || continue
    [ -n "$(find "$entry" -maxdepth 0 -mmin "-${age_cutoff_min}" -print 2>/dev/null)" ] && continue  # too fresh
    [ -n "$(lsof +D "$entry" 2>/dev/null | head -1)" ] && continue  # something has it open
    created="$(stat -c '%w' "$entry" 2>/dev/null)"; created="${created%%.*}"
    modified="$(stat -c '%y' "$entry" 2>/dev/null)"; modified="${modified%%.*}"
    if [ "$PRUNE_UNKNOWN_SCRATCH" = "1" ]; then
      status="deleted"
      if [ "$DRY_RUN" = "1" ]; then echo "  would remove (unknown, ${size_mb} MB, >${UNKNOWN_SCRATCH_STALE_DAYS}d): ${entry/#$CALLER_USER_HOME/\~}"; status="would delete"
      else del "$entry"; fi
    else
      status="flagged only"
    fi
    UNKNOWN_SCRATCH_HITS+=("$entry"$'\t'"${size_mb} MB"$'\t'"${created:-unknown}"$'\t'"${modified:-unknown}"$'\t'"$status")
  done
}
print_unknown_scratch_report() {
  [ "${#UNKNOWN_SCRATCH_HITS[@]}" -gt 0 ] || return 0
  echo ""
  if [ "$PRUNE_UNKNOWN_SCRATCH" = "1" ]; then
    echo "🗒️  Unknown-scratch catch-all -- deleted (name matched no known pattern):"
  else
    echo "🗒️  Unknown-scratch catch-all -- FLAGGED ONLY, not deleted (pass --prune-unknown-scratch to delete):"
  fi
  printf '%-10s  %-20s  %-20s  %s\n' "SIZE" "CREATED" "MODIFIED" "PATH"
  local _hit _h_path _h_size _h_created _h_modified _h_status
  for _hit in "${UNKNOWN_SCRATCH_HITS[@]}"; do
    IFS=$'\t' read -r _h_path _h_size _h_created _h_modified _h_status <<<"$_hit"
    printf '%-10s  %-20s  %-20s  %s  [%s]\n' "$_h_size" "$_h_created" "$_h_modified" "${_h_path/#$CALLER_USER_HOME/\~}" "$_h_status"
  done
}

if [ "$ONLY_UNKNOWN_SCRATCH" = "1" ]; then
  echo "🗒️  --only-unknown-scratch: running just the name-blind catch-all"
  sweep_unknown_large_scratch "$CALLER_USER_HOME/.local/share/opencode/tmp"
  for _vf_t in /private/var/folders/*/*/T; do
    [ -d "$_vf_t" ] || continue
    sweep_unknown_large_scratch "$_vf_t"
  done
  if [ "${#UNKNOWN_SCRATCH_HITS[@]}" -eq 0 ]; then
    echo "nothing >=${UNKNOWN_SCRATCH_MIN_MB}MB and >${UNKNOWN_SCRATCH_STALE_DAYS}d old in either scratch root"
  else
    print_unknown_scratch_report
  fi
  exit 0
fi

#############################################################################
# HALF 1 — caches, logs, package managers
#############################################################################
if [ "$DO_CACHES" = "1" ]; then

echo "🐳 docker"
if have docker; then
  if ! docker info >/dev/null 2>&1; then
    info "docker daemon not reachable -- every prune below would silently no-op, skipping"
    info "        (\`colima start\` first if you want container/image/volume space back)"
  elif [ -n "$DOCKER_AGE_DAYS" ]; then
    # Age-scoped mode: reclaim inside the VM without destroying it. `until` is
    # a native filter for containers/images; volumes have no such filter, so
    # they are inspected individually. --volumes is deliberately NOT passed to
    # `system prune` here -- that ignores age entirely and would take live data.
    _h=$((DOCKER_AGE_DAYS * 24))
    echo "   age-scoped: removing containers/images unused for >${DOCKER_AGE_DAYS}d"
    run docker container prune --force --filter "until=${_h}h" || warn "container prune failed"
    run docker image prune --all --force --filter "until=${_h}h" || warn "image prune failed"
    run docker builder prune --force --filter "until=${_h}h"     || warn "builder prune failed"
    # Dangling volumes only, and only those old enough. A volume still attached
    # to any container is never listed by `-f dangling=true`.
    _cut=$(( $(date +%s) - DOCKER_AGE_DAYS * 86400 ))
    while IFS= read -r v; do
      [ -n "$v" ] || continue
      _c=$(docker volume inspect "$v" --format '{{.CreatedAt}}' 2>/dev/null)
      _ts=$(date -j -f "%Y-%m-%dT%H:%M:%S" "${_c%%Z*}" +%s 2>/dev/null || echo 0)
      [ "${_ts:-0}" -gt 0 ] && [ "$_ts" -lt "$_cut" ] && run docker volume rm "$v" >/dev/null
    done < <(docker volume ls -q -f dangling=true 2>/dev/null)
  else
    run docker container prune --force || warn "docker container prune failed"
    run docker builder prune --force   || warn "docker builder prune failed"
    run docker system prune --force --all --volumes || warn "docker system prune failed"
    # Belt and suspenders on the volume side: measured here, `system prune
    # --volumes` (and even a standalone `docker volume prune --force`) left a
    # compose-labeled, zero-container, zero-byte volume in place while
    # reporting "Total reclaimed space: 0B" -- a `docker volume rm` on the
    # same volume name succeeded immediately. Whatever `dangling=true` the
    # prune commands use internally, it isn't catching every volume `docker
    # volume ls -f dangling=true` itself lists. This also matters beyond the
    # volume's own bytes: the colima recreate check below requires zero
    # volumes, and one leftover (however small) was blocking a 100 GB+ sparse
    # VM disk from ever being offered for recreation. Not run in the
    # age-scoped branch above: that one already removes dangling volumes
    # directly, but filtered by age -- doing it again here unconditionally
    # would defeat --docker-age's whole point for volumes specifically.
    _dangling_removed=0
    while IFS= read -r _v; do
      [ -n "$_v" ] || continue
      run docker volume rm "$_v" >/dev/null 2>&1 && _dangling_removed=$((_dangling_removed + 1))
    done < <(docker volume ls -q -f dangling=true 2>/dev/null)
    [ "$_dangling_removed" -gt 0 ] && info "docker: removed $_dangling_removed dangling volume(s) system prune left behind"
  fi
else
  info "docker not installed, skipping"
fi

# --- colima -----------------------------------------------------------------
# The VM's sparse disks never shrink: `docker system prune -a` frees blocks
# inside the guest, but the host-side file keeps them allocated. Measured 4.5 GB
# of disk images against 0 images / 0 containers / 0 volumes. There is no
# compaction command (`colima prune` only drops cached downloaded assets), so
# recreating the VM is the only way to reclaim it. Guarded on the VM being
# genuinely empty.
#
# EVERY profile is handled, not just `default`. colima supports `-p NAME`, and
# each profile is an independent VM with its own disk, socket and containers.
# Summing `du ~/.colima` across all of them while only ever running
# `colima delete -f` (which targets `default`) meant a second profile's size
# could trigger deletion of the FIRST profile's VM. Each is now sized, probed
# and deleted by name.
#   profile `default` -> lima instance `colima`,     disk _lima/_disks/colima
#   profile `foo`     -> lima instance `colima-foo`, disk _lima/_disks/colima-foo
colima_profiles() {
  local d b
  for d in "$CALLER_USER_HOME"/.colima/*/; do
    [ -d "$d" ] || continue
    b="$(basename "$d")"
    case "$b" in _lima|_store|_templates) continue ;; esac
    printf '%s\n' "$b"
  done
}

if have colima && have docker; then
  _any_profile=0
  while IFS= read -r prof; do
    [ -n "$prof" ] || continue
    _any_profile=1
    if [ "$prof" = "default" ]; then inst="colima"; else inst="colima-$prof"; fi
    sock="$CALLER_USER_HOME/.colima/$prof/docker.sock"
    disk="$CALLER_USER_HOME/.colima/_lima/_disks/$inst"
    [ -d "$disk" ] || disk="$CALLER_USER_HOME/.colima/_lima/$inst"
    size_kb="$(du -sk "$disk" 2>/dev/null | cut -f1)"; size_kb="${size_kb:-0}"

    # The container/volume counts are only trustworthy when the daemon actually
    # answers. A stopped VM makes `docker ps -q` print nothing, wc -l reports 0,
    # and that reads as "empty" -- the `${x:-1}` fallbacks cannot save us,
    # because the variable holds the literal string "0", not empty. So a stopped
    # VM full of real containers and volumes looked exactly like an empty one,
    # and got deleted without verification. Probe that profile's socket first.
    if DOCKER_HOST="unix://$sock" docker info >/dev/null 2>&1; then
      nc="$(DOCKER_HOST="unix://$sock" docker ps -q 2>/dev/null | wc -l | tr -d ' ')"
      nv="$(DOCKER_HOST="unix://$sock" docker volume ls -q 2>/dev/null | wc -l | tr -d ' ')"
      reachable=1
    else
      nc=""; nv=""; reachable=0
    fi

    if [ "$reachable" = "0" ]; then
      # Can't verify emptiness without starting the VM -- but a STOPPED,
      # non-default profile whose disk has not been written to in
      # COLIMA_STOPPED_STALE_DAYS is unambiguous: nothing has run inside it
      # (writes touch the disk file) in that whole window, verified or not.
      # `default` is never considered here regardless of age -- it is the
      # one profile virtually everything assumes exists.
      disk_mtime="$(date -r "$disk" +%s 2>/dev/null || echo 0)"
      cutoff=$(( $(date +%s) - COLIMA_STOPPED_STALE_DAYS * 86400 ))
      if [ "$prof" != "default" ] && [ "$disk_mtime" -gt 0 ] && [ "$disk_mtime" -lt "$cutoff" ]; then
        echo "🐳 colima[$prof]: stopped, disk untouched >${COLIMA_STOPPED_STALE_DAYS}d ($(human_size $((size_kb * 1024)))) — deleting"
        run as_user colima delete -f -p "$prof" || warn "colima delete -p $prof failed"
      else
        info "colima[$prof]: daemon not reachable -- cannot verify it is empty, leaving it alone"
        info "              (\`colima start -p $prof\` if you want this VM reclaimed)"
      fi
    elif [ "${nc:-1}" = "0" ] && [ "${nv:-1}" = "0" ] && [ "$size_kb" -gt 1048576 ]; then
      echo "🐳 colima[$prof]: $(human_size $((size_kb * 1024))) of disk, 0 containers, 0 volumes — recreating"
      run as_user colima stop -p "$prof"      || warn "colima stop -p $prof failed"
      run as_user colima delete -f -p "$prof" || warn "colima delete -p $prof failed"
      if [ "$DRY_RUN" = "1" ]; then echo "  would run: colima start -p $prof (detached)"
      else
        as_user nohup colima start -p "$prof" >"/tmp/colima-clean-start-$prof.log" 2>&1 &
        disown 2>/dev/null || true
        info "colima start -p $prof running detached (log: /tmp/colima-clean-start-$prof.log)"
      fi
    else
      info "colima[$prof]: ${nc} containers, ${nv} volumes, $(human_size $((size_kb * 1024))) — leaving VM alone"
    fi
  done < <(colima_profiles)
  [ "$_any_profile" = "0" ] && info "colima: no profiles provisioned, nothing to reclaim"
else
  info "colima/docker not both present, skipping VM recreate"
fi

# --- Tier 1: purely derived, no network state -------------------------------
echo "🧽 Tier 1 caches"
del "$CALLER_USER_HOME/Library/Developer/Xcode/DerivedData"
del "$CALLER_USER_HOME/.cache/gh" "$CALLER_USER_HOME/.cache/zig" \
    "$CALLER_USER_HOME/.cache/cmux" "$CALLER_USER_HOME/.cache/huggingface" \
    "$CALLER_USER_HOME/.cache/opencode.bak"
for _l in "$CALLER_USER_HOME"/.cache/fff_mcp*.log; do [ -e "$_l" ] && del "$_l"; done
del "$CALLER_USER_HOME/Library/Application Support/Code/Cache" \
    "$CALLER_USER_HOME/Library/Application Support/Code/CachedData" \
    "$CALLER_USER_HOME/Library/Application Support/Cursor/Cache" \
    "$CALLER_USER_HOME/Library/Application Support/Cursor/CachedData"
# .m2 may hold internal Artifactory artifacts that are gone upstream. Included
# deliberately: if the registry dropped them, they are not wanted either.
del "$CALLER_USER_HOME/.m2"
del "$CALLER_USER_HOME/.copilot/pkg" "$CALLER_USER_HOME/.copilot/logs"
del "$CALLER_USER_HOME/.codedb/projects"
del "$CALLER_USER_HOME/Library/Application Support/rancher-desktop"
del "$CALLER_USER_HOME/Library/Caches" "$CALLER_USER_HOME/Library/Logs"
del "$ROOT_HOME/Library/Caches" "$ROOT_HOME/Library/Logs"
del /Library/Caches/Homebrew
del /var/vm/sleepimage

# --- Tier 2: native prune, never rm -rf --------------------------------------
# These know which entries are still referenced. Blanket removal would force a
# full redownload and, for pnpm, break hardlinked node_modules already on disk.
echo "🧽 Tier 2 caches (native prune)"
prune_with() {
  local label="$1"; shift
  local bin="$1"
  if as_user_have "$bin"; then
    run as_user "$@" || warn "$label prune failed"
  else
    info "$label not installed, skipping"
  fi
}
prune_with npm   npm cache verify
# uv: bounded wait for the cache lock. Every `uv run` holds a shared lock on
# ~/.cache/uv for as long as its program runs, and `uv cache prune` waits for
# all of them -- so one long-lived `uv run python main.py` (seen 2026-10-02,
# running since 02:43) stalled clean indefinitely at "Cache is currently
# in-use, waiting for other uv processes to finish". UV_LOCK_TIMEOUT makes uv
# give up instead (verified: exits 2 after the timeout with "Timeout (3s) when
# waiting for lock"). Not --force: that prunes without the lock, underneath
# whatever is running.
if as_user_have uv; then
  run as_user env UV_LOCK_TIMEOUT=30 uv cache prune ||
    warn "uv prune skipped: cache still locked after 30s by a running uv process (pgrep -fl 'uv run')"
else
  info "uv not installed, skipping"
fi
prune_with pnpm  pnpm store prune      # NEVER rm -rf: breaks hardlinked node_modules in place
prune_with yarn  yarn cache clean
prune_with go    go clean -modcache
prune_with cargo-cache cargo cache -r all   # consider --autoclean-expensive for a gentler pass
prune_with pip   pip cache purge
prune_with pip3  pip3 cache purge
prune_with pipx  pipx uninstall-all --help  # probe only; destructive variants left out
prune_with deno  deno clean
prune_with composer composer clear-cache
prune_with gem   gem cleanup
prune_with nuget dotnet nuget locals all --clear
prune_with gradle_stop gradle --stop

# Both homebrew prefixes: nix-homebrew with enableRosetta=true provisions an
# arm64 prefix (/opt/homebrew) AND an x86_64 Rosetta prefix (/usr/local). Only
# cleaning whichever one PATH happens to resolve leaves the other untouched.
for _bp in /opt/homebrew/bin/brew /usr/local/bin/brew; do
  [ -x "$_bp" ] || continue
  echo "🍺 brew cleanup: $("$_bp" --prefix 2>/dev/null)"
  run as_user "$_bp" cleanup --prune=all || warn "brew cleanup failed for $_bp"
done
# bun refuses to run outside a project, so hand it a throwaway one.
if as_user_have bun; then
  if [ "$DRY_RUN" = "1" ]; then echo "  would run: bun pm cache rm"
  else
    BUN_TMP="$(mktemp -d)"; chown "$CALLER_USER" "$BUN_TMP"
    as_user sh -c "cd '$BUN_TMP' && printf '{\"name\":\"t\",\"version\":\"1.0.0\"}' > package.json && bun pm cache rm" \
      || warn "bun cache prune failed"
    rm -rf "$BUN_TMP"
  fi
else
  info "bun not installed, skipping"
fi

# --- VS Code extensions -----------------------------------------------------
# There is no `code --prune`: the CLI only offers --list/--install/--uninstall,
# so superseded versions accumulate forever. Measured here: azureterraform had
# 0.7.0 AND 0.9.0 (1.7 GB), sonarlint two copies (739 MB) -- 1.62 GB total.
#
# Superseded versions are always removed: VS Code never re-creates an old
# version, so this is a one-way win.
#
# VSCODE_PRUNE_HEAVY additionally deletes the big extensions outright. It is
# OFF by default on purpose: VS Code re-downloads them on next launch, so a
# clean run would delete them again next time -- a perpetual redownload loop
# rather than a saving. Set VSCODE_PRUNE_HEAVY=1 for a one-off reclaim.
VSCODE_EXT_DIRS=(
  "$CALLER_USER_HOME/.vscode/extensions"
  "$CALLER_USER_HOME/.vscode-insiders/extensions"
  "$CALLER_USER_HOME/.cursor/extensions"
)
VSCODE_HEAVY_GLOBS=(
  'ms-*terraform-*' 'ms-dotnettools.*' 'ms-azuretools.*'
  'codestream.codestream-*' 'sonarsource.*' 'asf.*' 'redhat.*'
)
prune_vscode_extensions() {
  local dir="$1" base old freed=0 k
  [ -d "$dir" ] || return 0
  # Superseded versions: strip the trailing -X.Y.Z[-platform] to group, keep
  # the highest by version sort.
  while IFS= read -r base; do
    local vers=(); while IFS= read -r v; do vers+=("$v"); done \
      < <(ls -1d "$dir/$base"-* 2>/dev/null | sort -V)
    [ ${#vers[@]} -lt 2 ] && continue
    for old in "${vers[@]:0:$((${#vers[@]}-1))}"; do
      k=$(du -sk "$old" 2>/dev/null | cut -f1); freed=$((freed + ${k:-0}))
      del "$old"
    done
  done < <(ls -1 "$dir" 2>/dev/null | sed -E 's/-[0-9]+\.[0-9]+\.[0-9]+(-.*)?$//' | sort | uniq -d)
  info "$(basename "$(dirname "$dir")"): superseded versions freed $((freed / 1024)) MB"

  if [ "${VSCODE_PRUNE_HEAVY:-0}" = "1" ]; then
    local g
    for g in "${VSCODE_HEAVY_GLOBS[@]}"; do
      for old in "$dir"/$g; do [ -d "$old" ] && del "$old"; done
    done
    info "$(basename "$(dirname "$dir")"): heavy globs removed (will redownload)"
  fi
}
echo "🧩 VS Code extensions"
for _d in "${VSCODE_EXT_DIRS[@]}"; do prune_vscode_extensions "$_d"; done

# --- orphaned brew casks / mas apps -----------------------------------------
# Casks present on disk but absent from casks.nix are drift: usually a rename
# (handbrake vs handbrake-app) leaving BOTH installed, or something commented
# out that was never uninstalled because homebrew.onActivation.cleanup="none".
# Reported, never auto-removed: deleting an app is not a cache operation.
if [ -n "$(real_brew)" ] && [ -f "$SELF_DIR/casks.nix" ]; then
  _b="$(real_brew)"
  # `brew list --cask` prints BARE names (docker-desktop) while casks.nix may
  # declare them tap-qualified (homebrew/cask/docker-desktop). Comparing the two
  # verbatim reported declared casks as orphans -- which is not a cosmetic bug,
  # it is an instruction to uninstall something the config asks for. Strip the
  # tap prefix from the declared side before comparing. Same shape as the
  # tap-qualified entries in brews.nix (gromgit/fuse/bindfs-mac).
  _orphans=$(comm -23 \
    <(as_user "$_b" list --cask 2>/dev/null | sort) \
    <(grep -oE '^[[:space:]]*"[^"]+"' "$SELF_DIR/casks.nix" | tr -d ' "' | sed 's|.*/||' | sort) 2>/dev/null)
  if [ -n "$_orphans" ]; then
    warn "casks installed but not in casks.nix (uninstall with: $_b uninstall --cask <name>)"
    printf '     %s\n' $_orphans
  fi
  # Casks whose app bundle is gone: the cask is registered but there is nothing
  # installed. Reported, never auto-removed -- an app can legitimately live
  # outside /Applications. Deliberately NOT wired to an --uninstall-orphans
  # flag: the test is "absent from casks.nix", and a temporarily commented-out
  # entry would then delete a working app. visual-studio-code is exactly that
  # case today.
  _dead=""
  for _c in $(as_user "$_b" list --cask 2>/dev/null); do
    _app=$(as_user "$_b" info --cask "$_c" 2>/dev/null | grep -oE '/Applications/[^ ]*\.app' | head -1)
    [ -n "$_app" ] && [ ! -d "$_app" ] && _dead="$_dead $_c"
  done
  [ -n "$_dead" ] && { warn "casks installed but the app is MISSING from disk:"; printf '     %s\n' $_dead; }
fi
if have mas && [ -f "$SELF_DIR/masApps.nix" ]; then
  _masorph=$(comm -23 \
    <(as_user mas list 2>/dev/null | awk '{$1=""; sub(/ *\([^)]*\)$/,""); sub(/^ /,""); print}' | sort) \
    <(grep -oE '"[^"]+" = [0-9]+' "$SELF_DIR/masApps.nix" | sed -E 's/" = .*//; s/^"//' | sort) 2>/dev/null)
  [ -n "$_masorph" ] && { warn "App Store apps not in masApps.nix:"; printf '     %s\n' $_masorph; }
fi

# --- Other language/tool caches, user + system ------------------------------
# Present-but-unlisted tools get cleaned; absent ones are informational only.
echo "🧽 Language/tool caches"
for _rel in \
  .gradle/caches .nuget/packages .dotnet .gem .bundle .cache/pip .ivy2/cache \
  .sbt .stack-work .cocoapods/repos .pub-cache .deno .cache/yarn .cache/Homebrew \
  .rustup/toolchains/.tmp .cache/ms-playwright .cache/puppeteer .cache/electron \
  .cache/node-gyp .cache/typescript .cache/deno .cache/prisma .cache/JetBrains
do
  for _base in "$CALLER_USER_HOME" "$ROOT_HOME"; do
    [ -d "$_base/$_rel" ] && del "$_base/$_rel"
  done
done
for _sys in /Library/Caches/com.apple.dt.Xcode /System/Volumes/Data/Library/Caches; do
  [ -d "$_sys" ] && del "$_sys"
done

# --- Tier 3: stale-gated nix caches -----------------------------------------
# nix-collect-garbage manages /nix/store only; it never touches ~/.cache/nix,
# so this grows without bound.
echo "🧽 Tier 3 caches (older than ${CACHE_STALE_DAYS}d)"
for _c in "$CALLER_USER_HOME"/.cache/nix/eval-cache-*; do
  [ -d "$_c" ] || continue
  if [ -z "$(find "$_c" -type f -mtime "-${CACHE_STALE_DAYS}" -print -quit 2>/dev/null)" ]; then
    del "$_c"
  fi
done

# tarball-cache is a BARE GIT REPO of fetched flake inputs. `gc --prune` alone
# reclaims almost nothing, because nix writes a ref per input and refs keep
# objects reachable forever -- regardless of whether any flake.lock still points
# at them. To actually age it out the stale REFS must go first; then
# git-maintain-repos --deep does the repack and prune.
# Safe: if a flake.lock still pins an expired input, nix simply refetches it.
TARBALL_CACHE="$CALLER_USER_HOME/.cache/nix/tarball-cache"
if [ -d "$TARBALL_CACHE" ]; then
  cutoff=$(( $(date +%s) - CACHE_STALE_DAYS * 86400 ))
  expired=0
  while IFS=' ' read -r ref ts; do
    [ -n "$ref" ] || continue
    if [ "${ts:-0}" -lt "$cutoff" ]; then
      if [ "$DRY_RUN" = "1" ]; then echo "  would expire ref: $ref"
      else as_user git -C "$TARBALL_CACHE" update-ref -d "$ref" 2>/dev/null && expired=$((expired + 1)); fi
    fi
  done < <(as_user git -C "$TARBALL_CACHE" for-each-ref \
             --format='%(refname) %(committerdate:unix)' 2>/dev/null)
  info "tarball-cache: expired $expired refs older than ${CACHE_STALE_DAYS}d"
  run "$SELF_DIR/git-maintain-repos" --path "$TARBALL_CACHE" --no-recursive --no-system --deep --no-register \
    || warn "tarball-cache maintenance failed"
fi

# Stale flake GC roots: a `nix build` result symlink or direnv profile in a
# directory nobody has touched pins its whole closure in /nix/store. Dropping
# the root lets the next nix-collect-garbage reclaim it.
echo "🧽 Stale flake GC roots (>${CACHE_STALE_DAYS}d)"
gcroot_dir=/nix/var/nix/gcroots/auto
if [ -d "$gcroot_dir" ]; then
  freed=0
  while IFS= read -r link; do
    target="$(readlink "$link" 2>/dev/null)" || continue
    [ -n "$target" ] || continue
    # The root points at a result symlink / .direnv profile inside a project.
    if [ ! -e "$target" ]; then
      del "$link"; freed=$((freed + 1)); continue
    fi
    # Probe the PROJECT, not the directory the symlink happens to sit in.
    #
    # nix-direnv lays out two kinds of root: .direnv/flake-profile-<hash> (the
    # devShell closure) and .direnv/flake-inputs/<hash>-source (one per flake
    # input). Taking dirname of the latter lands on flake-inputs/, which holds
    # nothing but symlinks -- and `find -type f` cannot see a symlink, so the
    # probe returned empty and the root read as stale no matter how active the
    # project was. Measured here: 44 of 50 direnv roots were being released
    # "stale by default" rather than on evidence.
    #
    # Walking up to the directory CONTAINING .direnv gives both kinds the same
    # owner and probes real source files. Cost of a wrong release is only a
    # `direnv reload`, but the decision should still be evidence-based.
    case "$target" in
      */.direnv/*) owner_dir="${target%%/.direnv/*}" ;;
      *)           owner_dir="$(dirname "$target")" ;;
    esac
    case "$target" in
      */.direnv/*|*/result|*/result-*)
        if [ -z "$(find "$owner_dir" -maxdepth 2 -type f -mtime "-${CACHE_STALE_DAYS}" -print -quit 2>/dev/null)" ]; then
          del "$link" "$target"; freed=$((freed + 1))
        fi
        ;;
    esac
  done < <(find "$gcroot_dir" -maxdepth 1 -type l 2>/dev/null)
  info "flake GC roots released: $freed"
fi

# nix garbage collection runs LAST of the nix work, deliberately: expiring
# tarball-cache refs and releasing stale flake GC roots above is what makes
# paths collectable, and doing this first would defer every one of those
# reclaims to the NEXT run.
#
# nix-collect-garbage vs `nix store gc`: same collector underneath. The old CLI
# additionally understands -d (delete old profile generations), which is where
# most reclaimable space actually comes from; `nix store gc` is still flagged
# experimental and has no generation handling. Use both -- -d for generations,
# then a plain sweep for anything left unreferenced.
# -d deletes EVERY old generation, so the only rollback target left is the one
# currently active -- a bad switch then has nowhere to go back to. Keeping a
# window costs little (generations share almost all their store paths) and
# preserves rollback. NIX_KEEP_DAYS=0 restores the old scorched-earth -d.
# nix has no single flag for "newer than N days AND at least M generations":
# --delete-older-than is purely age-based, so a quiet fortnight would leave the
# current generation as the only rollback target. Widen the age window until at
# least NIX_KEEP_GENS old generations survive, then hand that to nix.
# NIX_KEEP_DAYS=0 restores the old scorched-earth -d.
NIX_KEEP_DAYS="${NIX_KEEP_DAYS:-7}"
NIX_KEEP_GENS="${NIX_KEEP_GENS:-5}"

nix_keep_window() {
  local profile="$1" days="$NIX_KEEP_DAYS" kept
  local now cutoff
  now=$(date +%s)
  while [ "$days" -lt 3650 ]; do
    cutoff=$(( now - days * 86400 ))
    kept=0
    while IFS= read -r line; do
      # "  42   2026-08-01 12:00:00   (current)"
      local gdate; gdate=$(echo "$line" | awk '{print $2" "$3}')
      local gts; gts=$(date -j -f "%Y-%m-%d %H:%M:%S" "$gdate" +%s 2>/dev/null) || continue
      [ "$gts" -ge "$cutoff" ] && kept=$((kept + 1))
    done < <(nix-env --list-generations --profile "$profile" 2>/dev/null)
    # kept includes the current generation, so require KEEP_GENS + 1
    [ "$kept" -ge $(( NIX_KEEP_GENS + 1 )) ] && break
    days=$(( days * 2 ))
  done
  printf '%s' "$days"
}

if [ "$NIX_KEEP_DAYS" = "0" ]; then
  echo "🧊 nix store garbage collection (deleting ALL old generations)"
  run nix-collect-garbage -d || warn "nix-collect-garbage failed"
else
  _win="$(nix_keep_window /nix/var/nix/profiles/system)"
  if [ "$_win" != "$NIX_KEEP_DAYS" ]; then
    info "widened generation window ${NIX_KEEP_DAYS}d -> ${_win}d to keep >=${NIX_KEEP_GENS} generations"
  fi
  echo "🧊 nix store garbage collection (keeping last ${_win}d / >=${NIX_KEEP_GENS} generations)"
  run nix-collect-garbage --delete-older-than "${_win}d" \
    || warn "nix-collect-garbage --delete-older-than failed"
fi
run nix-store --gc || warn "nix-store --gc failed"

# --- Deliberately left alone ------------------------------------------------
# ~/.cache/opencode  - live runtime package cache (~420 MB) for the editor in
#                      daily use; removing it mid-session is not worth it.
# ~/.whisper/models  - static ASR weights (~6.2 GB). Artifacts, not a cache:
#                      mtime never advances on load, so staleness cannot tell
#                      "unused" from "loaded right now". If space is ever
#                      needed, gate on a long window (180d+) or delete a
#                      specific model by name; they refetch from HuggingFace at
#                      multi-GB cost.
# ~/.m2 corporate    - see note above; included by explicit request.
# ~/.cargo/git       - a force-pushed or deleted git dependency cannot come back.
# ~/.local/share/opencode/opencode.db - session database, not a cache.

#############################################################################
# HALF 1b — logs
#############################################################################
echo "🧻 logs"
# --- pytest tmp_path factory runs -------------------------------------------
# pytest's tmp_path/tmpdir fixtures create $TMPDIR/pytest-of-<user>/pytest-<N>/
# and keep only the last few (tmp_path_retention_count, default 3). They are
# therefore self-limiting at rest -- measured 16-20 MB here across 7 runs. But
# a suite IN FLIGHT can hold gigabytes: this directory was 2.0 GB mid-run and
# 16 MB a minute later.
#
# That is why the rule is age-based and NOT "keep only the newest". Deleting
# every run but the newest would destroy the tmpdir of any suite running
# concurrently -- including a second suite started moments ago -- and pytest
# gives no lock to detect that. Anything older than PYTEST_KEEP_HOURS cannot
# belong to a live run, so that window is the safe equivalent. The newest run
# is additionally always kept, so `pytest-current` never dangles.
PYTEST_KEEP_HOURS="${PYTEST_KEEP_HOURS:-4}"
_pt_removed=0
for _ptbase in "$CALLER_USER_HOME/.local/share/opencode/tmp" "${TMPDIR:-/tmp}" /tmp /private/var/folders/*/*/T; do
  [ -d "$_ptbase" ] || continue
  for _ptdir in "$_ptbase"/pytest-of-*; do
    [ -d "$_ptdir" ] || continue
    # Newest real run (ignore the pytest-current symlink) is always preserved.
    _newest="$(find "$_ptdir" -maxdepth 1 -mindepth 1 -type d -name 'pytest-*' \
                 -exec stat -c '%Y %n' {} \; 2>/dev/null | sort -rn | head -1 | cut -d' ' -f2-)"
    while IFS= read -r _run; do
      [ -n "$_run" ] || continue
      [ "$_run" = "$_newest" ] && continue
      del "$_run"; _pt_removed=$((_pt_removed + 1))
    done < <(find "$_ptdir" -maxdepth 1 -mindepth 1 -type d -name 'pytest-*' \
               -mmin "+$((PYTEST_KEEP_HOURS * 60))" 2>/dev/null)
    # A pytest-current symlink whose target we just removed is dead weight.
    [ -L "$_ptdir/pytest-current" ] && [ ! -e "$_ptdir/pytest-current" ] && del "$_ptdir/pytest-current"
    rmdir "$_ptdir" 2>/dev/null   # only succeeds if it is now empty
  done
done
[ "$_pt_removed" -gt 0 ] && info "pytest: removed $_pt_removed run dirs older than ${PYTEST_KEEP_HOURS}h"

del "$CALLER_USER_HOME/git/oseries"
OPS_LOG_DIR="$CALLER_USER_HOME/.cache/operations-portal/logs"
if [ -d "$OPS_LOG_DIR" ]; then
  # operations-portal rotates daily but never prunes: retention_days is defined
  # in config yet never read by app/utils/logging.py, so rotated logs accumulate
  # forever. With a DEBUG default level this reached 60 GB.
  if [ "$LOG_STALE_DAYS" -eq 0 ]; then
    del "$OPS_LOG_DIR"
  else
    if [ "$DRY_RUN" = "1" ]; then echo "  would delete logs older than ${LOG_STALE_DAYS}d in $OPS_LOG_DIR"
    else
      find "$OPS_LOG_DIR" -type f -mtime "+${LOG_STALE_DAYS}" -delete 2>/dev/null
      find "$OPS_LOG_DIR" -mindepth 1 -type d -empty -delete 2>/dev/null
    fi
  fi
fi

echo "🧹 short-lived scratch (tool-prefixed single-run dirs, 2 locations)"
# Same junk, two unrelated locations: ~/.local/share/opencode/tmp (opencode's
# own scratch convention) and /private/var/folders/*/*/T (the real $TMPDIR,
# otherwise reached only by the opt-in --tmpdir wipe below). Whichever TMPDIR
# a given subprocess inherits decides which pile it lands in, so both need
# the same sweep. Hundreds of single-run fixture/profile/build dirs
# accumulate in each (mcpx-fake-NNNN, mw_ctrl_test_XXXXXXXX,
# telemetry_test_XXXXXXXX, playwright-artifacts-XXXXXX,
# openchamber-*-XXXXXX...), each named by a tool whose own lifecycle is
# "create at process start, delete at process end" -- a Go t.TempDir(), a
# Python tempfile.mkdtemp(), a Playwright/OpenChamber artifact dir. One
# surviving past a few hours means the process that made it crashed or was
# killed before its own cleanup ran; nothing legitimate holds these open for
# half a day. That is what makes an ON-BY-DEFAULT short TTL safe here, unlike
# --tmpdir (which cannot tell a crashed process's leftovers from a live one's
# and so stays opt-in and all-or-nothing).
#
# Matched by NAME PREFIX only. Generic mktemp output (tmp.XXXXXXXX,
# tmpXXXXXXXX) and short/generic prefixes (oc-XXXXXX, seen holding what looks
# like a sandboxed throwaway $HOME) carry no reliably-unique tool identity,
# so "safe to delete" cannot be inferred from the name alone -- those are
# left for --tmpdir / manual review rather than risking a false match.
SHORT_SCRATCH_STALE_HOURS="${SHORT_SCRATCH_STALE_HOURS:-8}"

# sweep_short_lived_scratch BASE -- stale by TOP-LEVEL mtime only, in ONE find
# pass across the whole directory, not a `find` per entry. Measured at
# ~/.local/share/opencode/tmp: >12,000 entries, and a per-entry `find -mmin
# -quit` subprocess (cheap on its own) turned into minutes of pure fork
# overhead at that count. A single pass costs under a tenth of a second for
# the same answer: which top-level names were touched inside the window,
# checked once, then looked up in bash for every glob match.
#
# Own-mtime, not contents: these fixtures are written once at creation and
# abandoned, not rewritten in place, so a directory's own mtime (bumped by
# adding/removing/renaming entries inside it) is sufficient evidence here.
# The postgres-cluster sweep below needs the stronger contents-aware check
# instead, because postgres rewrites existing files without renaming them.
_ss_total_removed=0
sweep_short_lived_scratch() {
  local base="$1" removed=0
  [ -d "$base" ] || return 0
  local -A fresh=()
  local f entry name g match
  while IFS= read -r -d '' f; do
    fresh["${f##*/}"]=1
  done < <(find "$base" -maxdepth 1 -mindepth 1 -mmin "-$((SHORT_SCRATCH_STALE_HOURS * 60))" -print0 2>/dev/null)
  for entry in "$base"/*; do
    [ -e "$entry" ] || continue
    name="${entry##*/}"
    short_scratch_is_kept "$name" && continue
    [ -n "${fresh[$name]:-}" ] && continue   # touched inside the window: keep
    match=0
    for g in "${SHORT_SCRATCH_GLOBS[@]}"; do
      # shellcheck disable=SC2053
      [[ "$name" == $g ]] && { match=1; break; }
    done
    [ "$match" = 1 ] || continue
    del "$entry"; removed=$((removed + 1))
  done
  # Stale uv cache locks: a crashed `uv` leaves one of these behind, which is
  # exactly what makes the `uv cache prune` tier above wait out its own
  # UV_LOCK_TIMEOUT instead of running.
  while IFS= read -r lock; do
    del "$lock"; removed=$((removed + 1))
  done < <(find "$base" -maxdepth 1 -name 'uv-*.lock' -mmin "+$((SHORT_SCRATCH_STALE_HOURS * 60))" 2>/dev/null)
  _ss_total_removed=$((_ss_total_removed + removed))
  [ "$removed" -gt 0 ] && info "short-lived scratch (${base/#$CALLER_USER_HOME/\~}): removed $removed entries older than ${SHORT_SCRATCH_STALE_HOURS}h"
}
sweep_short_lived_scratch "$CALLER_USER_HOME/.local/share/opencode/tmp"
for _vf_t in /private/var/folders/*/*/T; do
  [ -d "$_vf_t" ] || continue
  sweep_short_lived_scratch "$_vf_t"
done
[ "$_ss_total_removed" -eq 0 ] && info "short-lived scratch: nothing older than ${SHORT_SCRATCH_STALE_HOURS}h in either location"

# unknown_scratch_is_known() / sweep_unknown_large_scratch() are defined
# earlier (scope-independent, alongside SHORT_SCRATCH_GLOBS) so
# --only-unknown-scratch can call them without running anything above first.
sweep_unknown_large_scratch "$CALLER_USER_HOME/.local/share/opencode/tmp"
for _vf_t in /private/var/folders/*/*/T; do
  [ -d "$_vf_t" ] || continue
  sweep_unknown_large_scratch "$_vf_t"
done
[ "${#UNKNOWN_SCRATCH_HITS[@]}" -gt 0 ] && info "unknown-scratch catch-all: ${#UNKNOWN_SCRATCH_HITS[@]} entries >=${UNKNOWN_SCRATCH_MIN_MB}MB and >${UNKNOWN_SCRATCH_STALE_DAYS}d old (see report at the end)$([ "$PRUNE_UNKNOWN_SCRATCH" != "1" ] && echo " -- flagged only, pass --prune-unknown-scratch to delete")"

# Ad hoc postgres data dirs (by PG_VERSION marker -- same signal
# tidy_purge_postgres_dirs uses for tmp/opencode) landing directly in either
# scratch root above, e.g. a stray `heron-once-pg`. Same 8h window as the
# test fixtures above, by request -- not left at a longer "idle overnight"
# default, on the theory that a RUNNING postgres keeps this safe regardless:
# checkpointer writes WAL/data files every few minutes by default even with
# zero query traffic, so a live cluster's own mtime almost never goes 8h
# stale in the first place, and the lsof open-handle check below catches the# rest (the listening socket file alone counts as an open handle). The
# window only bites a cluster whose postgres process is actually gone.
# Checked recursively, unlike the sweep above: postgres rewrites existing
# files (WAL, data pages) without renaming them, so the cluster directory's
# own mtime can go stale while writes are still landing inside it. There are
# at most a handful of these on a whole machine, so a `find` + `lsof` per
# match costs nothing.
PG_SCRATCH_STALE_HOURS="${PG_SCRATCH_STALE_HOURS:-8}"
sweep_ad_hoc_postgres_dirs() {
  local base="$1" marker pgdir
  [ -d "$base" ] || return 0
  while IFS= read -r marker; do
    pgdir="$(dirname "$marker")"
    case "$pgdir" in "$base/opencode"/*) continue ;; esac # owned by the tidy pass, not here
    if [ -z "$(find "$pgdir" -type f -not -name "$SPOTLIGHT_MARKER" -mmin "-$((PG_SCRATCH_STALE_HOURS * 60))" -print -quit 2>/dev/null)" ] &&
       [ -z "$(lsof +D "$pgdir" 2>/dev/null | head -1)" ]; then
      if [ "$DRY_RUN" = "1" ]; then echo "  would remove (postgres data dir, idle >${PG_SCRATCH_STALE_HOURS}h): ${pgdir/#$CALLER_USER_HOME/\~}"
      else echo "  removing (postgres data dir, idle >${PG_SCRATCH_STALE_HOURS}h): ${pgdir/#$CALLER_USER_HOME/\~}"; del "$pgdir"; fi
    fi
  done < <(find "$base" -maxdepth 2 -name PG_VERSION -type f 2>/dev/null)
}
sweep_ad_hoc_postgres_dirs "$CALLER_USER_HOME/.local/share/opencode/tmp"
for _vf_t in /private/var/folders/*/*/T; do
  [ -d "$_vf_t" ] || continue
  sweep_ad_hoc_postgres_dirs "$_vf_t"
done

echo "🧻 /private/var/folders"
# OPT-IN (--tmpdir). This is the LIVE per-user temp directory: $TMPDIR resolves
# into it, and processes running right now hold state there. Measured 349 MB
# here against 42 GB free -- not worth taking out from under a running session.
#
# It is also where scratch git worktrees end up when something ignores the
# durable worktree root, and the OS sweeps this path on its own schedule, so
# anything here is already living on borrowed time. Wiping it by hand just
# makes the loss immediate instead of eventual.
if [ "$CLEAN_TMPDIR" != "1" ]; then
  info "/private/var/folders skipped (pass --tmpdir to clear it; ~350 MB, wipes live \$TMPDIR)"
elif [ "$DRY_RUN" = "1" ]; then
  echo "  would clear /private/var/folders (keeping zz)"
else
  # The sweep itself lives in var-folders-clean.sh so its test
  # (var-folders-clean.test.sh, run by the flake check) exercises the
  # shipped logic instead of a pasted copy (#557).
  bash "$SELF_DIR/var-folders-clean.sh" /private/var/folders
fi

del "$CALLER_USER_HOME/.Trash/"* "$ROOT_HOME/.Trash/"*

fi  # DO_CACHES

#############################################################################
# HALF 2 — per-repo work, then maintenance
#############################################################################
if [ "$DO_REPOS" = "1" ]; then

DARGS=()
if [ -n "$TARGET_PATH" ]; then DARGS+=(--path "$TARGET_PATH"); else DARGS+=(--auto --user "$CALLER_USER"); fi
[ -n "$DEPTH_LIMIT" ] && DARGS+=(--depth "$DEPTH_LIMIT")
[ -n "$FOLLOW" ] && { [ "$FOLLOW" = "1" ] && DARGS+=(--follow) || DARGS+=(--no-follow); }

# In auto/full mode, assert the global git scheduler first. Basically a no-op
# when already configured. Skipped for --path, which is single-repo scoped.
if [ "$DO_MAINTENANCE" = "1" ] && [ -z "$TARGET_PATH" ]; then
  echo "🔧 git maintenance: system"
  run "$SELF_DIR/git-maintain-repos" --system || warn "maintenance --system failed"
fi

DISCOVERY_FILE="$(mktemp)"
REPOS_ONLY_FILE="$(mktemp)"
OWNERS_FILE="$(mktemp)"
trap 'rm -f "$DISCOVERY_FILE" "$REPOS_ONLY_FILE" "$OWNERS_FILE"' EXIT

# ONE discovery pass feeds everything below: .trash removal, artifact pruning,
# Spotlight markers and the maintenance repo list.
echo "🔎 discovering repositories and artifacts"
"$SELF_DIR/git-discover-repos" "${DARGS[@]}" --find all --status > "$DISCOVERY_FILE" 2>/dev/null \
  || warn "discovery failed"
awk -F'\t' '$1=="repo"{print $2}' "$DISCOVERY_FILE" > "$REPOS_ONLY_FILE"
# Artifact ownership is resolved against repos AND worktrees, while maintenance
# still runs against REPOS_ONLY_FILE alone. Keeping these separate matters: a
# linked worktree has no real repo as a path prefix, so resolving ownership from
# the repo list alone never matched anything under one. Every artifact dir in
# every worktree was therefore only Spotlight-marked and never pruned -- 32 GB
# of .venv/node_modules here, 28 GB of it under a single nested container.
# Worktrees must NOT enter the maintenance loop: they share the parent's object
# store, so maintaining each would repeat identical work N times.
awk -F'\t' '$1=="repo"||$1=="worktree"{print $2}' "$DISCOVERY_FILE" > "$OWNERS_FILE"
n_repos=$(wc -l < "$REPOS_ONLY_FILE" | tr -d ' ')
n_wt=$(awk -F'\t' '$1=="worktree"' "$DISCOVERY_FILE" | wc -l | tr -d ' ')
n_found=$(awk -F'\t' '$1=="FOUND"' "$DISCOVERY_FILE" | wc -l | tr -d ' ')
echo "   $n_repos repos, $n_wt worktrees, $n_found artifact dirs"

# Is a repo/worktree safe to prune artifacts from? Two independent gates.
#
# 1. mtime: no source file modified within ARTIFACT_STALE_DAYS. Artifact dirs,
#    .git and .worktrees are excluded from the probe; nested repos are evaluated
#    as their own units.
#
# 2. clean working tree. This is an ACTIVITY proxy, not a safety gate -- every
#    .venv/node_modules here is gitignored (measured: 0 of 74 git-tracked), so
#    removing one cannot destroy tracked work, committed or otherwise. It is
#    here because mtime alone is not a usable activity signal for worktrees:
#    `git worktree add`, `checkout` and rebase stamp EVERY file they write with
#    the current time, so a tree that was merely materialised looks identical to
#    one being actively edited. Measured here: one worktree had all 7846 of its
#    files inside a single 60-second window. Uncommitted changes are the part
#    that reliably means "someone is mid-task".
#
# Deliberately NOT gated on unpushed commits: 29 of 50 worktrees here have no
# upstream at all and 5 are detached, so `log @{u}..HEAD` cannot be evaluated
# for most of them -- it silently reports 0 and reads as a safety signal it is
# not. Irrelevant to artifact deletion in any case, per the note above.
git_tree_is_clean() {
  [ -e "$1/.git" ] || return 0     # not a repo/worktree: nothing git can say
  [ -z "$(git -C "$1" status --porcelain 2>/dev/null | head -1)" ]
}
repo_is_idle() {
  git_tree_is_clean "$1" || return 1
  [ -z "$(find "$1" \
      \( "${CACHE_EXPR[@]}" -o "${RISK_EXPR[@]}" -o -name .git -o -name .worktrees \) -prune -o \
      -type f -mtime "-${ARTIFACT_STALE_DAYS}" -print -quit 2>/dev/null)" ]
}

# A dirty/active OWNING repo still protects its SOURCE from repo_is_idle, but
# a cache-type artifact (.venv, node_modules, __pycache__, ...) is never
# source -- it is what repo_is_idle's own comment already says, gitignored
# and incapable of holding tracked work. So an artifact gets a second,
# independent path to deletion even when its owning repo reads as active:
# its OWN contents are stale, AND nothing has it open right now.
#
# Deliberately its OWN window, not ARTIFACT_STALE_DAYS: that one also decides
# whether the REPO is idle (gates the --deep git-maintenance downgrade and
# the fully-idle artifact path), a slower-moving signal worth keeping at a
# day. A build cache sitting untouched in an otherwise-dirty repo is safe to
# reclaim much sooner than that -- hours, not days.
ACTIVE_ARTIFACT_STALE_HOURS="${ACTIVE_ARTIFACT_STALE_HOURS:-8}"
#
# The second check matters specifically because staleness alone is not
# enough evidence for .venv/node_modules the way it is for source files: a
# long-running `uv run` dev server can hold a venv open for days without ever
# touching a file inside it, so an mtime check alone would delete out from
# under a live process. lsof's cost is proportional to system-wide open
# files, not the target directory's size, so this stays cheap whether the
# artifact is a two-file cache or a 50,000-file venv.
#
# Excludes the Spotlight marker itself from the staleness check: the first
# time this artifact was judged not-yet-prunable, mark_dir() touched
# .metadata_never_index inside it -- a file this script wrote, not evidence
# of anyone using the artifact. Without the exclusion, that one touch would
# make the directory look "recently modified" for a full
# ACTIVE_ARTIFACT_STALE_HOURS after every mark, purely because of the
# bookkeeping file clean.sh itself left behind. Verified against a real
# worktree's .venv: its only file inside the window was its own marker.
artifact_is_prunable() {
  local path="$1"
  [ -n "$(find "$path" -type f -not -name "$SPOTLIGHT_MARKER" -mmin "-$((ACTIVE_ARTIFACT_STALE_HOURS * 60))" -print -quit 2>/dev/null)" ] && return 1
  [ -n "$(lsof +D "$path" 2>/dev/null | head -1)" ] && return 1
  return 0
}

# Spotlight markers are non-destructive and reversible, so they are applied
# regardless of staleness -- and re-applied to the empty stub left behind after
# a delete, so the directory is already excluded when a build regenerates it.
mark_dir() {
  local d="$1"
  [ -d "$d" ] || return 0
  # App static trees may be committed or feed flat-static manifests. Never add
  # an untracked Spotlight marker to them.
  [ "${d##*/}" = "static" ] && return 0
  [ -e "$d/$SPOTLIGHT_MARKER" ] && return 0
  if [ "$DRY_RUN" = "1" ]; then echo "  would mark: ${d/#$HOME/\~}"; MARKED=$((MARKED + 1)); return 0; fi
  spotlight_mark "$d" "$CALLER_USER" && MARKED=$((MARKED + 1))
}
# rm -rf then recreate is FASTER than emptying in place: `find -delete` stats and
# traverses every entry, while rm -rf uses the bulk unlink path. The stub costs
# two syscalls.
#
# Except virtualenvs, which are deleted outright with no stub. uv (and venv
# tooling generally) treats an existing .venv without pyvenv.cfg as a broken
# environment and refuses to use or replace it, so the empty stub, marker and
# all, made every cleaned project fail until the directory was removed by hand.
# They lose the Spotlight marker until the next clean re-marks the new one.
NO_STUB_NAMES=(.venv venv)
del_and_stub() {
  del "$1"
  local _n
  for _n in "${NO_STUB_NAMES[@]}"; do
    [ "${1##*/}" = "$_n" ] && return 0
  done
  if [ "$DRY_RUN" != "1" ]; then
    mkdir -p "$1" 2>/dev/null && chown "$CALLER_USER" "$1" 2>/dev/null
  fi
  mark_dir "$1"
}

MARKED=0; TRASHED=0; PRUNED=0; SKIPPED_TRACKED=0; NONGIT=0
declare -A IDLE_CACHE=()

echo "🗑  .trash, artifacts, Spotlight markers"
while IFS=$'\t' read -r tag reason status path; do
  [ "$tag" = "FOUND" ] || continue
  case "$reason" in
    trash)
      # Discard piles go unconditionally, staleness irrelevant.
      del "$path"; TRASHED=$((TRASHED + 1))
      ;;
    cache)
      owner="${path}"; owner_repo=""
      # Longest matching repo OR worktree prefix wins, so an artifact inside a
      # worktree is judged against that worktree rather than falling through to
      # mark-only.
      while IFS= read -r r; do
        case "$path" in "$r"/*) [ ${#r} -gt ${#owner_repo} ] && owner_repo="$r" ;; esac
      done < "$OWNERS_FILE"
      if [ -n "$owner_repo" ]; then
        if [ -z "${IDLE_CACHE[$owner_repo]:-}" ]; then
          if repo_is_idle "$owner_repo"; then IDLE_CACHE[$owner_repo]=idle; else IDLE_CACHE[$owner_repo]=active; fi
        fi
        if [ "${IDLE_CACHE[$owner_repo]}" = "idle" ]; then
          del_and_stub "$path"; PRUNED=$((PRUNED + 1))
        elif artifact_is_prunable "$path"; then
          # Repo is dirty or recently touched, but THIS artifact (not source)
          # is independently stale and nothing has it open: uncommitted work
          # in the repo is not a reason to keep its .venv/node_modules too.
          del_and_stub "$path"; PRUNED=$((PRUNED + 1))
        else
          mark_dir "$path"       # active repo, artifact itself not safely prunable: mark but never delete
        fi
      else
        # No owning repo/worktree: a plain directory tree. There is no git
        # state to consult, so mtime is the only evidence available -- but that
        # is no reason to never prune. Previously these were marked and kept
        # forever, so a .venv outside a repo could not be reclaimed at all.
        # Judged against its own parent, using the same staleness window.
        parent="$(dirname "$path")"
        if [ -z "${IDLE_CACHE[$parent]:-}" ]; then
          if [ -z "$(find "$parent" \
               \( "${CACHE_EXPR[@]}" -o "${RISK_EXPR[@]}" -o -name .git -o -name .worktrees \) -prune -o \
               -type f -mtime "-${ARTIFACT_STALE_DAYS}" -print -quit 2>/dev/null)" ]; then
            IDLE_CACHE[$parent]=idle
          else IDLE_CACHE[$parent]=active; fi
        fi
        if [ "${IDLE_CACHE[$parent]}" = "idle" ]; then
          del_and_stub "$path"; PRUNED=$((PRUNED + 1)); NONGIT=$((NONGIT + 1))
        else
          mark_dir "$path"
        fi
      fi
      ;;
    risky)
      # Only removable when git proves the whole dir is ignored. Tracked dirs
      # are not even marked: an untracked marker inside a committed vendor/
      # would show up in every `git status`.
      if [ "$status" = "tracked" ] || [ "$status" = "staged" ]; then
        SKIPPED_TRACKED=$((SKIPPED_TRACKED + 1))
      elif [ "$status" = "ignored" ]; then
        del_and_stub "$path"; PRUNED=$((PRUNED + 1))
      else
        mark_dir "$path"
      fi
      ;;
  esac
done < "$DISCOVERY_FILE"

# Mark every discovered object store too.
while IFS=$'\t' read -r kind path; do
  case "$kind" in repo|worktree) mark_dir "$path/.git" ;; esac
done < <(awk -F'\t' '$1=="repo"||$1=="worktree"{print $1"\t"$2}' "$DISCOVERY_FILE")

echo "   $TRASHED .trash removed, $PRUNED artifact dirs pruned ($NONGIT outside any repo), $MARKED marked, $SKIPPED_TRACKED tracked skipped"

# Ad hoc postgres data dirs INSIDE a discovered repo or worktree (by
# PG_VERSION marker, same signal as the top-level scratch sweep above). Not
# name-based like CACHE_NAMES -- there is no one conventional name (seen:
# heron-once-pg, pgnative/data, pgdata), only the marker file every initdb
# cluster writes -- so this is its own pass over $OWNERS_FILE rather than
# something git-discover-repos's name list can express. Gated the same way
# the main artifact loop now is: the repo being dirty is not a reason to keep
# a postgres cluster nobody has touched in PG_SCRATCH_STALE_HOURS and nothing
# has open. The search is NOT depth-bounded (#375): a cluster nested deeper
# than the old maxdepth 4 inside a worktree (e.g. services/api/.data/pgdata)
# was silently missed. Cost is bounded by pruning instead: .git and
# node_modules -- the two trees that dominate a worktree walk -- are never
# descended into. .worktrees is pruned too (#411): every linked worktree is
# its own line in $OWNERS_FILE, so descending into it from the repo root
# only walked (and idle-checked) the same tree a second time; the sibling
# artifact passes above prune it for the same reason. The -d guard covers
# a marker whose cluster was already removed as part of an enclosing
# cluster earlier in this same pass.
_repo_pg_removed=0
while IFS= read -r _owner; do
  [ -d "$_owner" ] || continue
  while IFS= read -r _marker; do
    _pgdir="$(dirname "$_marker")"
    [ -d "$_pgdir" ] || continue
    if [ -z "$(find "$_pgdir" -type f -not -name "$SPOTLIGHT_MARKER" -mmin "-$((PG_SCRATCH_STALE_HOURS * 60))" -print -quit 2>/dev/null)" ] &&
       [ -z "$(lsof +D "$_pgdir" 2>/dev/null | head -1)" ]; then
      if [ "$DRY_RUN" = "1" ]; then echo "  would remove (postgres data dir in repo, idle >${PG_SCRATCH_STALE_HOURS}h): ${_pgdir/#$CALLER_USER_HOME/\~}"
      else echo "  removing (postgres data dir in repo, idle >${PG_SCRATCH_STALE_HOURS}h): ${_pgdir/#$CALLER_USER_HOME/\~}"; del "$_pgdir"; fi
      _repo_pg_removed=$((_repo_pg_removed + 1))
    fi
  done < <(find "$_owner" \( -type d \( -name .git -o -name node_modules -o -name .worktrees \) -prune \) -o -name PG_VERSION -type f -print 2>/dev/null)
done < "$OWNERS_FILE"
[ "$_repo_pg_removed" -gt 0 ] && info "postgres data dirs inside repos/worktrees: removed $_repo_pg_removed"

# --- Artifact dirs directly in ~, outside any repo --------------------------
# ~/.cache is deliberately skipped: it is the shared XDG cache (uv, nix, gh,
# huggingface, opencode) with its own policy above, not a build artifact.
if [ -z "$TARGET_PATH" ]; then
  echo "🏠 artifact dirs directly in ~"
  HOME_ARTIFACT_SKIP=(.cache)
  for _n in "${CACHE_NAMES[@]}"; do
    _d="$CALLER_USER_HOME/$_n"
    [ -d "$_d" ] || continue
    _skip=0
    for _s in "${HOME_ARTIFACT_SKIP[@]}"; do [ "$_n" = "$_s" ] && _skip=1; done
    [ "$_skip" = "1" ] && { info "~/$_n skipped by policy"; continue; }
    if [ -z "$(find "$_d" -type f -mtime "-${HOME_ARTIFACT_STALE_DAYS}" -print -quit 2>/dev/null)" ]; then
      del_and_stub "$_d"
    else
      mark_dir "$_d"
    fi
  done
fi

# --- opencode scratch dirs: tmp/opencode entries & worktree/ ---------------
# ~/.local/share/opencode/tmp/opencode is a flat scratch space every session
# writes loose files and directories into (probe scripts, screenshots, ad hoc
# `initdb` postgres clusters, source checkouts...) with nothing to prune it.
# ~/.local/share/opencode/worktree holds one directory per git worktree a
# session created; sessions end without removing them. Measured here: 231
# worktree dirs (59 GB) and 678 tmp/opencode entries (45 GB), almost none
# touched by any automated cleanup before this.
#
# Both are gated the same way repo_is_idle gates artifact dirs: a dirty
# working tree or commits not yet pushed upstream mean "someone is mid-task"
# and must block deletion regardless of age. Only entries that are git repos
# get that check -- most tmp/opencode entries are plain scratch files with no
# .git, where mtime is the only signal available. Age is checked last, and
# deliberately on its own env vars: these are session work products a user
# might still want to reference, not pure build cache, so the default window
# is wider than ARTIFACT_STALE_DAYS.
OPENCODE_TMP_STALE_DAYS="${OPENCODE_TMP_STALE_DAYS:-7}"
OPENCODE_WORKTREE_STALE_DAYS="${OPENCODE_WORKTREE_STALE_DAYS:-7}"

opencode_entry_is_idle() {
  local path="$1" days="$2" up
  if [ -e "$path/.git" ]; then
    git_tree_is_clean "$path" || return 1
    up="$(as_user git -C "$path" rev-parse --abbrev-ref --symbolic-full-name '@{u}' 2>/dev/null)"
    if [ -n "$up" ]; then
      [ -z "$(as_user git -C "$path" log '@{u}..HEAD' --oneline 2>/dev/null)" ] || return 1
    fi
  fi
  if [ -d "$path" ]; then
    [ -z "$(find "$path" -mindepth 1 \( "${CACHE_EXPR[@]}" -o "${RISK_EXPR[@]}" -o -name .git \) -prune -o \
        -type f -mtime "-${days}" -print -quit 2>/dev/null)" ]
  else
    [ -z "$(find "$path" -maxdepth 0 -mtime "-${days}" -print -quit 2>/dev/null)" ]
  fi
}

# A caller-new worktree or tmp entry is never at risk: `git worktree add` /
# `git checkout` stamp every file they write with the CURRENT time (noted
# above repo_is_idle), so a tree materialised minutes ago has nothing older
# than the staleness window and opencode_entry_is_idle's mtime probe finds
# that immediately and blocks it -- regardless of how clean or "even with
# main" it is. Age is necessary as well as git-clean for exactly this reason.
#
# Every deletion is logged by path (not just counted) so a run is auditable
# after the fact: `grep '^  removing:' <clean output>`.
clean_opencode_scratch_dir() {
  local base="$1" days="$2" label="$3" n=0 freed_kb=0 entry k
  [ -d "$base" ] || return 0
  for entry in "$base"/*; do
    [ -e "$entry" ] || continue
    if opencode_entry_is_idle "$entry" "$days"; then
      k="$(du -sk "$entry" 2>/dev/null | cut -f1)"
      if [ "$DRY_RUN" = "1" ]; then echo "  would remove: ${entry/#$CALLER_USER_HOME/\~} ($(( ${k:-0} / 1024 )) MB)"
      else echo "  removing: ${entry/#$CALLER_USER_HOME/\~} ($(( ${k:-0} / 1024 )) MB)"; del "$entry"; fi
      n=$((n + 1)); freed_kb=$((freed_kb + ${k:-0}))
    fi
  done
  info "$label: removed $n entries older than ${days}d ($((freed_kb / 1024)) MB)"
}

# Stray one-off dirs sessions sometimes drop directly in opencode/ itself
# (ad hoc postgres clusters, recovery dumps...) rather than under tmp/ --
# same idle gate, scoped away from the app's own structural files/dirs so a
# future opencode release adding a new top-level file is never swept blind.
OPENCODE_ROOT_STALE_DAYS="${OPENCODE_ROOT_STALE_DAYS:-7}"
OPENCODE_ROOT_PROTECTED=(
  .DS_Store "$SPOTLIGHT_MARKER" .opencode
  account.json auth.json backups
  opencode.db opencode.db-shm opencode.db-wal
  opencode-v2.db opencode-v2.db-shm opencode-v2.db-wal
  opencode-next.db opencode-next.db-shm opencode-next.db-wal
  log repos shell snapshot storage tmp tool-output worktree
)
clean_opencode_root_scratch() {
  local base="$1" days="$2" n=0 freed_kb=0 entry name p skip k
  [ -d "$base" ] || return 0
  for entry in "$base"/*; do
    [ -e "$entry" ] || continue
    name="$(basename "$entry")"
    skip=0
    for p in "${OPENCODE_ROOT_PROTECTED[@]}"; do [ "$name" = "$p" ] && skip=1 && break; done
    [ "$skip" = "1" ] && continue
    if opencode_entry_is_idle "$entry" "$days"; then
      k="$(du -sk "$entry" 2>/dev/null | cut -f1)"
      if [ "$DRY_RUN" = "1" ]; then echo "  would remove: ${entry/#$CALLER_USER_HOME/\~} ($(( ${k:-0} / 1024 )) MB)"
      else echo "  removing: ${entry/#$CALLER_USER_HOME/\~} ($(( ${k:-0} / 1024 )) MB)"; del "$entry"; fi
      n=$((n + 1)); freed_kb=$((freed_kb + ${k:-0}))
    fi
  done
  info "opencode/ root: removed $n stray entries older than ${days}d ($((freed_kb / 1024)) MB)"
}

# Tidy only, never a deletion decision in the git-gated sense -- but unlike a
# plain rename, this DOES delete the heaviest content outright (postgres data
# dirs, anything oversized) before archiving what's left, so it still needs
# the same "nothing touched recently, nowhere" guarantee repo_is_idle relies
# on elsewhere.
#
# tmp/opencode is a flat mix of real scratch directories (worktree-like
# checkouts, projects someone is mid-task in) and loose plain files (probe
# scripts, .out/.log/.png captures, patches) scattered at every depth, not
# just the top level. Three rules, in order:
#
#   1. A directory that IS a git repo/worktree (has .git at its own root) is
#      never touched by tidy at all -- it is left for the existing idle +
#      age gate in clean_opencode_scratch_dir to judge on its own schedule.
#   2. Otherwise, if ANY file anywhere in its subtree was modified within
#      OPENCODE_TIDY_PROTECT_DAYS, the WHOLE directory is skipped -- an
#      active task's directory is never partially reorganised.
#   3. Otherwise, any worktree buried somewhere inside is judged by
#      OPENCODE_WORKTREE_STALE_DAYS -- the same gate the top-level worktree/
#      sweep uses -- not the (much shorter) tidy window above, since a
#      worktree someone is still using can easily outlast it. If that
#      worktree is still kept (dirty, unpushed, or inside its own stale
#      window), it protects the WHOLE containing directory: nothing here is
#      purged or archived. If every nested worktree found is idle by that
#      same rule, it is removed outright (same as an idle top-level
#      worktree) and the directory proceeds as a normal leaf.
#   4. A leaf with no kept worktree inside it is true scratch: oversized/
#      postgres content is deleted from it outright, then the (now much
#      smaller) remainder is archived as a single unit into
#      _loose/<year>/<month>/.
#
# Loose plain files sitting directly in tmp/opencode's own root are filed
# into _loose/<year>/<month>/ the same way, individually.
#
# Bucketing never changes when something becomes eligible for deletion: the
# age gate elsewhere keys off each file's own mtime recursively, never a
# container directory's, and `date -r` (used throughout here) reads a FILE's
# own mtime -- never today's date -- so re-running this never reshuffles
# anything already filed and an archived leaf still ages out normally.
OPENCODE_TIDY_PROTECT_DAYS="${OPENCODE_TIDY_PROTECT_DAYS:-1}"
OPENCODE_TIDY_LARGE_MB="${OPENCODE_TIDY_LARGE_MB:-200}"

tidy_dir_has_recent_file() {
  # .git internals are excluded: `git status` (run by opencode_entry_is_idle,
  # by clean.sh's own repo maintenance, by anything) touches .git/index's
  # mtime on every invocation, read-only or not. Counting that would make any
  # directory holding a repo look "recently modified" indefinitely, on a
  # timer reset by code that has nothing to do with this directory's own
  # content.
  [ -n "$(find "$1" -mindepth 1 -name .git -prune -o -type f -mtime "-${OPENCODE_TIDY_PROTECT_DAYS}" -print -quit 2>/dev/null)" ]
}

# initdb always writes PG_VERSION at a postgres data dir's root, regardless
# of what the dir is named (pgdata, pgm2, heron-once-pg's data dir, ...) or
# how big it got -- a much more reliable signal than name or size alone.
tidy_purge_postgres_dirs() {
  local root="$1" marker pgdir
  while IFS= read -r marker; do
    [ -n "$marker" ] || continue
    pgdir="$(dirname "$marker")"
    [ -d "$pgdir" ] || continue   # already removed as part of an enclosing cluster this pass
    # Same in-use guard as the sibling postgres sweeps: the caller's
    # recent-file gate does not protect an idle-but-running server, which
    # can write nothing for >OPENCODE_TIDY_PROTECT_DAYS while still
    # holding its data dir open.
    if [ -n "$(lsof +D "$pgdir" 2>/dev/null | head -1)" ]; then
      echo "  keeping (postgres data dir in use): ${pgdir/#$CALLER_USER_HOME/\~}"
      continue
    fi
    if [ "$DRY_RUN" = "1" ]; then echo "  would remove (postgres data dir): ${pgdir/#$CALLER_USER_HOME/\~}"
    else echo "  removing (postgres data dir): ${pgdir/#$CALLER_USER_HOME/\~}"; del "$pgdir"; fi
    # (the find below prunes .git/node_modules/.worktrees, mirroring the
    # per-repo sweep's prune set from #411: repo internals, dependency
    # trees and linked worktrees are not scratch this pass owns)
  done < <(find "$root" \( -type d \( -name .git -o -name node_modules -o -name .worktrees \) -prune \) -o -name PG_VERSION -type f -print 2>/dev/null)
}
# One level deep only, deliberately: venvs/build caches/binary dumps are
# almost always the scratch dir's direct children, and a full recursive du
# over everything already-kept would be O(n^2) for no real benefit here.
tidy_purge_large_entries() {
  local root="$1" limit_kb=$(( OPENCODE_TIDY_LARGE_MB * 1024 )) entry k
  for entry in "$root"/*; do
    [ -e "$entry" ] || continue
    k="$(du -sk "$entry" 2>/dev/null | cut -f1)"
    if [ "${k:-0}" -ge "$limit_kb" ]; then
      if [ "$DRY_RUN" = "1" ]; then echo "  would remove (>${OPENCODE_TIDY_LARGE_MB}MB): ${entry/#$CALLER_USER_HOME/\~} ($(( k / 1024 )) MB)"
      else echo "  removing (>${OPENCODE_TIDY_LARGE_MB}MB): ${entry/#$CALLER_USER_HOME/\~} ($(( k / 1024 )) MB)"; del "$entry"; fi
    fi
  done
}

tidy_newest_file_in() {
  local f newest="" newest_t=0 t
  while IFS= read -r -d '' f; do
    t="$(date -r "$f" +%s 2>/dev/null)" || continue
    if [ "${t:-0}" -gt "$newest_t" ]; then newest_t="$t"; newest="$f"; fi
  done < <(find "$1" -type f -print0 2>/dev/null)
  printf '%s' "$newest"
}

tidy_file_to_bucket() {
  local entry="$1" bucket_root="$2" year month dest
  year="$(date -r "$entry" +%Y 2>/dev/null || echo unknown)"
  month="$(date -r "$entry" +%m 2>/dev/null || echo unknown)"
  dest="$bucket_root/$year/$month"
  if [ "$DRY_RUN" = "1" ]; then
    echo "  would file away: ${entry/#$CALLER_USER_HOME/\~} -> ${dest/#$CALLER_USER_HOME/\~}/"
  else
    mkdir -p "$dest" && mv "$entry" "$dest/" 2>/dev/null
  fi
}
tidy_dir_to_bucket() {
  local dir="$1" bucket_root="$2" ref year month dest
  ref="$(tidy_newest_file_in "$dir")"
  [ -z "$ref" ] && ref="$dir"   # nothing left inside after purging: use its own mtime
  year="$(date -r "$ref" +%Y 2>/dev/null || echo unknown)"
  month="$(date -r "$ref" +%m 2>/dev/null || echo unknown)"
  dest="$bucket_root/$year/$month"
  if [ "$DRY_RUN" = "1" ]; then
    echo "  would archive: ${dir/#$CALLER_USER_HOME/\~} -> ${dest/#$CALLER_USER_HOME/\~}/$(basename "$dir")/"
  else
    mkdir -p "$dest" && mv "$dir" "$dest/" 2>/dev/null
  fi
}

tidy_classify_and_place() {
  local dir="$1" bucket_root="$2" g wt kept_worktree="" idle_worktrees=()
  tidy_dir_has_recent_file "$dir" && return 0   # rule 2, tidy's own protect window

  # Rule 3, worktree-bearing case: a nested worktree's own kept/idle status
  # is judged by OPENCODE_WORKTREE_STALE_DAYS (the same gate the top-level
  # worktree/ sweep uses), not OPENCODE_TIDY_PROTECT_DAYS -- a worktree
  # someone is still using can easily be older than the tidy window without
  # being done. If ANY nested worktree is still kept (dirty, unpushed, or
  # not yet past its own stale window), it protects this WHOLE directory --
  # nothing here is purged, archived, or otherwise reorganised.
  while IFS= read -r g; do
    [ -n "$g" ] || continue
    wt="${g%/.git}"
    if opencode_entry_is_idle "$wt" "$OPENCODE_WORKTREE_STALE_DAYS"; then
      idle_worktrees+=("$wt")
    else
      kept_worktree="$wt"; break
    fi
  done < <(find "$dir" -mindepth 1 -name .git -print 2>/dev/null)

  if [ -n "$kept_worktree" ]; then
    return 0   # a kept worktree inside: leave this entire directory alone
  fi
  # Every nested worktree found (if any) is idle by its own rules -- it is no
  # longer protecting anything, so it is removed the same way an idle
  # top-level worktree would be, and this directory proceeds as a normal leaf.
  for wt in "${idle_worktrees[@]}"; do
    if [ "$DRY_RUN" = "1" ]; then echo "  would remove (idle nested worktree): ${wt/#$CALLER_USER_HOME/\~}"
    else echo "  removing (idle nested worktree): ${wt/#$CALLER_USER_HOME/\~}"; del "$wt"; fi
  done

  # rule 3, plain leaf: purge the heavy stuff, archive the rest as one unit
  tidy_purge_postgres_dirs "$dir"
  tidy_purge_large_entries "$dir"
  tidy_dir_to_bucket "$dir" "$bucket_root"
}

tidy_opencode_loose_files() {
  local base="$1" bucket_root="$2" entry name
  [ -d "$base" ] || return 0
  for entry in "$base"/*; do
    [ -e "$entry" ] || continue
    [ "$entry" = "$bucket_root" ] && continue
    name="$(basename "$entry")"
    if [ -f "$entry" ]; then
      [ "$name" = "$SPOTLIGHT_MARKER" ] && continue
      tidy_file_to_bucket "$entry" "$bucket_root"
    elif [ -d "$entry" ]; then
      [ -e "$entry/.git" ] && continue           # rule 1
      tidy_classify_and_place "$entry" "$bucket_root"
    fi
  done
}

if [ -z "$TARGET_PATH" ]; then
  echo "🗂  opencode scratch dirs (tmp/opencode >${OPENCODE_TMP_STALE_DAYS}d, worktree >${OPENCODE_WORKTREE_STALE_DAYS}d)"
  OC_SHARE="$CALLER_USER_HOME/.local/share/opencode"
  tidy_opencode_loose_files "$OC_SHARE/tmp/opencode" "$OC_SHARE/tmp/opencode/_loose"
  clean_opencode_scratch_dir "$OC_SHARE/tmp/opencode" "$OPENCODE_TMP_STALE_DAYS" "tmp/opencode"
  clean_opencode_scratch_dir "$OC_SHARE/worktree" "$OPENCODE_WORKTREE_STALE_DAYS" "worktree"

  # Second junk drawer: some sandboxed contexts default opencode's scratch
  # root to $TMPDIR instead of ~/.local/share/opencode, so the SAME pattern --
  # loose probe scripts, patches, dumped JSON, and live git worktrees -- piles
  # up again at /private/var/folders/*/*/T/opencode, untouched by anything
  # above or by --tmpdir (which wipes the whole var/folders tree rather than
  # reasoning about what's inside it). Found here holding real worktrees from
  # `git worktree list` (mainwt, remove-sqlite/...) alongside ~900 loose
  # files, so it gets the identical worktree-aware tidy pass, not a blind
  # sweep: tidy_opencode_loose_files skips anything with its own .git, and
  # clean_opencode_scratch_dir's idle check (clean tree, nothing unpushed,
  # past the age window) is what actually decides a worktree's fate.
  for _vf_oc in /private/var/folders/*/*/T/opencode; do
    [ -d "$_vf_oc" ] || continue
    tidy_opencode_loose_files "$_vf_oc" "$_vf_oc/_loose"
    clean_opencode_scratch_dir "$_vf_oc" "$OPENCODE_TMP_STALE_DAYS" "/private/var/folders T/opencode"
  done

  if [ "${CLEAN_OPENCODE_ROOT:-0}" = "1" ]; then
    echo "🗂  opencode/ root scratch (--opencode-root-scratch, >${OPENCODE_ROOT_STALE_DAYS}d)"
    clean_opencode_root_scratch "$OC_SHARE" "$OPENCODE_ROOT_STALE_DAYS"
  else
    info "opencode/ root scratch skipped (pass --opencode-root-scratch to sweep it, --dry-run first recommended)"
  fi
fi


# Recovery only: caller-repository maintenance runs as CALLER_USER, so this
# should be redundant. Limit any repair to Git metadata, never source trees.
git_metadata_needs_repair() {
  [ -n "$(find "$1" ! -user "$CALLER_USER" -print -quit 2>/dev/null)" ]
}

repair_git_ownership() {
  local kind path git_dir pointer caller_uid repaired=0
  caller_uid="$(id -u "$CALLER_USER")"
  while IFS=$'\t' read -r kind path; do
    [ -d "$path" ] || continue
    if [ -f "$path/HEAD" ] && [ -d "$path/objects" ] && [ -d "$path/refs" ]; then
      git_dir="$path"
    elif [ -f "$path/.git" ]; then
      pointer="$path/.git"
      git_dir="$(git -c safe.directory='*' -C "$path" rev-parse --absolute-git-dir 2>/dev/null)"
      [ -n "$git_dir" ] || { warn "could not resolve gitdir for $path"; continue; }
    elif [ -d "$path/.git" ]; then
      git_dir="$path/.git"
    else
      continue
    fi
    [ -d "$git_dir" ] || continue
    if { [ -n "${pointer:-}" ] && [ "$(stat -f '%u' "$pointer" 2>/dev/null)" != "$caller_uid" ]; } || git_metadata_needs_repair "$git_dir"; then
      if [ "$DRY_RUN" = "1" ]; then
        echo "  would repair Git ownership: ${git_dir/#$CALLER_USER_HOME/\~}"
      else
        [ -z "${pointer:-}" ] || chown -h "$CALLER_USER" "$pointer" 2>/dev/null || warn "could not repair $pointer"
        chown -R -h -P "$CALLER_USER" "$git_dir" 2>/dev/null || warn "could not repair $git_dir"
      fi
      repaired=$((repaired + 1))
    fi
    pointer=""
  done < <(awk -F'\t' '$1=="repo"||$1=="worktree"{print $1"\t"$2}' "$DISCOVERY_FILE")
  [ "$repaired" -gt 0 ] && echo "   $repaired Git metadata directories ownership repaired"
}

# Match the scope of the CHECK to the scope of the ACTION.
#
# Artifact deletion above is per-directory, so it is judged per repo/worktree:
# pruning one worktree's .venv cannot affect any other tree.
#
# `--deep` is different. It runs `gc --prune=now` against the object store,
# which a repo SHARES with every one of its linked worktrees -- 54 live ones on
# operations-portal here. Normal gc leaves a 2-week grace period on unreachable
# objects precisely so a concurrent writer cannot have an object pruned out from
# under it; `--prune=now` removes that grace entirely. So a deep pass is only
# safe when the repo AND every live worktree hanging off it are quiescent.
#
# When one is active the repo is DOWNGRADED to --safe, not skipped and not
# --quick: --safe still does the full repack, and only gives up the `--prune=now`
# grace-window removal, which is the single part of --deep that is unsafe next to
# a live worktree. --quick rewrites no packs at all, so the 24 repos downgraded
# on the last run reclaimed nothing; --safe reclaims almost all of it.
repo_tree_group_is_idle() {
  local repo="$1" wt
  repo_is_idle "$repo" || return 1
  while IFS= read -r wt; do
    [ -d "$wt" ] || continue          # registered but swept: nothing to check
    [ "$wt" = "$repo" ] && continue
    repo_is_idle "$wt" || return 1
  done < <(as_user git -C "$repo" worktree list --porcelain 2>/dev/null |
             awk '/^worktree /{p=substr($0,10)} /^prunable/{p=""} /^$/{if(p!="")print p; p=""}')
  return 0
}

# --- git maintenance, last: it is the slowest step --------------------------
if [ "$DO_MAINTENANCE" = "1" ] && [ "$n_repos" -gt 0 ]; then
  repair_git_ownership
  echo "🔧 git maintenance ($MAINT_DEPTH) across $n_repos repos"
  MAINT_EXTRA=()
  [ "$EXPIRE_REFLOG" = "1" ] && MAINT_EXTRA+=(--expire-reflog)
  DOWNGRADED=0
  while IFS= read -r repo; do
    [ -d "$repo" ] || continue
    # Stale worktree ADMIN entries (the worktree's own directory is gone, but
    # its registration under .git/worktrees/ survives) are invisible to
    # everything above -- they are not a repo, not a worktree with a living
    # directory, just dead metadata that accumulates forever otherwise.
    # Surfaced here as "could not resolve gitdir" warnings from
    # repair_git_ownership before this fix. Safe by git's own default: it
    # only removes an entry whose working directory is confirmably gone.
    if [ "$DRY_RUN" = "1" ]; then
      _wtp_out="$(as_user git -C "$repo" worktree prune -n -v 2>&1)"
      [ -n "$_wtp_out" ] && echo "  would prune worktree admin entries in ${repo/#$CALLER_USER_HOME/\~}:"$'\n'"$_wtp_out"
    else
      _wtp_out="$(as_user git -C "$repo" worktree prune -v 2>&1)"
      [ -n "$_wtp_out" ] && info "${repo/#$CALLER_USER_HOME/\~}: $_wtp_out"
    fi
    depth="$MAINT_DEPTH"
    if [ "$depth" = "--deep" ] && ! repo_tree_group_is_idle "$repo"; then
      depth="--safe"; DOWNGRADED=$((DOWNGRADED + 1))
      info "${repo/#$CALLER_USER_HOME/\~}: active repo or worktree -- --deep downgraded to --safe"
    fi
    run as_user "$SELF_DIR/git-maintain-repos" --path "$repo" --no-recursive --no-system \
      "$depth" --clean-orphans "${MAINT_EXTRA[@]+"${MAINT_EXTRA[@]}"}" || warn "maintenance failed for $repo"
  done < "$REPOS_ONLY_FILE"
  [ "$DOWNGRADED" -gt 0 ] && echo "   $DOWNGRADED repos downgraded to --safe (shared object store in use)"
fi

# --- root's own repositories ------------------------------------------------
# Root cannot have the git scheduler installed (launchd *agents* need a GUI
# session), so this is repo cleanup and config only.
if [ -z "$TARGET_PATH" ] && [ -d "$ROOT_HOME" ] && [ "$ROOT_HOME" != "$CALLER_USER_HOME" ]; then
  echo "🔧 root-owned repositories"
  root_repos=$("$SELF_DIR/git-discover-repos" --auto --repos-only --user root 2>/dev/null | cut -f2)
  if [ -n "$root_repos" ]; then
    while IFS= read -r repo; do
      [ -d "$repo" ] || continue
      run "$SELF_DIR/git-maintain-repos" --path "$repo" --no-recursive --no-system "$MAINT_DEPTH" \
        || warn "root maintenance failed for $repo"
    done <<< "$root_repos"
  else
    info "no repositories under root's home"
  fi
fi

fi  # DO_REPOS

# ---------------------------------------------------------------------------
DISK_FREE_END="$(get_free_bytes)"
echo ""
echo "📏 Free disk before clean: $(human_size "${DISK_FREE_START:-0}")"
echo "📏 Free disk after clean:  $(human_size "$DISK_FREE_END")"
RECLAIMED=$(( DISK_FREE_END - ${DISK_FREE_START:-0} ))
if (( RECLAIMED > 0 )); then echo "✅ Reclaimed: $(human_size "$RECLAIMED")"
else echo "⚠️  No net disk reclaimed ($(human_size $(( -RECLAIMED ))) more used)"; fi

# --- Unknown-scratch catch-all report, always last -------------------------
# print_unknown_scratch_report() is defined earlier (scope-independent,
# alongside sweep_unknown_large_scratch) so --only-unknown-scratch's
# short-circuit can print the identical report without duplicating it.
# Printed regardless of --prune-unknown-scratch: without it these are the
# candidates nobody has acted on yet; with it, this is the record of what
# just got deleted (size/dates captured before the delete, since the path is
# gone by the time this prints).
print_unknown_scratch_report
