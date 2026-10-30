#!/usr/bin/env bash
# SPDX-License-Identifier: MIT OR Apache-2.0
#
# common.sh — shared library for clean.sh, git-maintain-repos,
# spotlight-exclude-artifacts and git-discover-repos.
#
# Sourced, never executed. Holds the three directory-name lists, the find(1)
# fragment builder, the Spotlight marker helper, and the shared argument
# guard helpers -- everything more than one of those scripts needs to agree
# on.
#
# THE THREE LISTS
#   CACHE_NAMES  Regenerable caches/build output. No source ever lives here, so
#                clean.sh removes them outright once a repo looks idle.
#   RISKY_NAMES  Names that are USUALLY build output but are frequently
#                committed. Sampled in this tree: vendor 16/37, build 5/15,
#                dist 1/8, target 1/1 were git-tracked. clean.sh must prove a
#                dir is fully git-ignored before touching one.
#   TRASH_NAMES  Discard piles. Removed unconditionally wherever found.
#
# All three are pruned from repo discovery walks, so a vendored copy such as
# node_modules/*/.git is never mistaken for a real repository.

# Regenerable: caches, virtualenvs, build caches.
CACHE_NAMES=(
  node_modules .venv venv .cache
  __pycache__ .mypy_cache .ruff_cache .pytest_cache
  .tox .nox .direnv
  .turbo .next .nuxt .svelte-kit .parcel-cache
  .gradle .ipynb_checkpoints htmlcov .eggs
  .terraform
)

# Only removed when git confirms every file inside is ignored.
RISKY_NAMES=(vendor build dist target)

# Always removed.
TRASH_NAMES=(.trash)

# Everything that should be pruned from a discovery walk, or marked for
# Spotlight exclusion. .git is included by callers that want it.
ALL_ARTIFACT_NAMES=("${CACHE_NAMES[@]}" "${RISKY_NAMES[@]}" "${TRASH_NAMES[@]}")

# build_name_expr OUTVAR NAME...
#   Fills OUTVAR with a find(1) fragment: -name a -o -name b -o -name c
#   Callers wrap it: find X \( "${ARR[@]}" \) -prune ...
#   Using nameref keeps this bash 4.3+; every consumer already requires bash.
build_name_expr() {
  local -n _out="$1"
  shift
  _out=()
  local _n
  for _n in "$@"; do
    [ ${#_out[@]} -gt 0 ] && _out+=(-o)
    _out+=(-name "$_n")
  done
}

# ---------------------------------------------------------------------------
# The argument contract (design #1145, rules 1-8)
# ---------------------------------------------------------------------------
# Every script in this family parses under the same contract:
#   1. An unknown option exits 2, naming the option, with usage on stderr.
#   2. A value-taking option given a missing, empty or flag-shaped value
#      exits 2 naming the option ("<option> requires a value"). Parsing
#      completes fully before any work begins.
#   3. A single-value option given twice exits 2: "<option> may be given
#      only once". Repetition is legal only for options declared repeatable
#      in the script's usage, and those accumulate, never last-win.
#   4. A conflicting pair given together exits 2 naming both, in either
#      order (positive/negative, mode, and filter pairs alike).
#   5. An extra or duplicated positional exits 2, naming it (and the
#      already-set target where there is one).
#   6. Numeric values are validated at parse time as strict plain decimal:
#      digits only, explicitly base-10 (a leading zero is data, never
#      octal), with the declared range (positive, or non-negative where 0
#      is meaningful) and an INT_MAX backstop. An invalid number is a
#      parse error, never a mid-run failure or a silent collapse to 0.
#   7. Environment tunables are arguments: validated at startup under the
#      same rules, naming the variable.
#   8. --help exits 0, prints usage, and has no side effects; --dry-run is
#      an ordinary option under all the rules above.
#
# The five helpers below carry the guard mechanics; each script's
# vocabulary, defaults and mode logic stay in the script. A consumer sets
# ARG_PROGRAM (its message prefix, e.g. "clean.sh") after sourcing this
# file and defines usage() before its parser runs -- the helpers are only
# called from the parser, by which time usage() exists. The helpers raise
# no new bash requirement: namerefs are already used above (bash 4.3+).

ARG_PROGRAM="${ARG_PROGRAM:-${0##*/}}"

# arg_die MESSAGE...
#   The contract's die-with-usage: "<prog>: MESSAGE" on stderr, usage on
#   stderr, exit 2.
arg_die() {
  echo "$ARG_PROGRAM: $*" >&2
  usage >&2
  exit 2
}

# arg_require_value OPTION [VALUE]
#   Rule 2 guard for one case arm, called before the arm shifts:
#     arg_require_value "$1" "${2:-}"
arg_require_value() {
  [ -n "${2:-}" ] && [ "${2#-}" = "$2" ] || arg_die "$1 requires a value"
}

# arg_once OPTION SETVAR
#   Rule 3 guard: SETVAR names the caller's 0/1 seen-flag; the first use
#   marks it 1, a repeat dies with the contract phrase.
#     arg_once "$1" DEPTH_SET
arg_once() {
  local -n _arg_once_seen="$2"
  [ "${_arg_once_seen:-0}" -eq 0 ] || arg_die "$1 may be given only once"
  _arg_once_seen=1
}

# arg_conflict OPTION OTHER_OPTION SEEN
#   Rule 4 guard for a canonical pair: OPTION and OTHER_OPTION are named
#   in that (canonical) order regardless of which arm runs, and SEEN is
#   the other side's seen value (a 0/1 flag or a non-empty string); a
#   truthy SEEN dies naming both.
#     arg_conflict --system --no-system "$NO_SYSTEM_SET"
arg_conflict() {
  [ -z "${3:-}" ] || [ "$3" = "0" ] || arg_die "$1 and $2 may not be combined"
}

# arg_decimal OUTVAR LABEL VALUE RANGE
#   Rule 6 judge: VALUE must be digits only and within range -- RANGE is
#   "positive" (> 0) or "non-negative" (>= 0) -- with the INT_MAX
#   backstop judged by digit count first, so over-long digit strings are
#   rejected cleanly instead of tripping the shell's 64-bit arithmetic
#   (#858/#862 technique). On success OUTVAR holds the normalised decimal
#   (leading zeros stripped: the judged form is the used form, so a use
#   site doing bash arithmetic never reads a leading zero as octal,
#   #881). On failure it prints "<prog>: LABEL must be a RANGE integer
#   (got: VALUE)" and exits 2 -- without usage, matching the post-parse
#   judges this helper replaces.
arg_decimal() {
  local _arg_decimal_label="$2" _arg_decimal_value="$3" _arg_decimal_range="$4"
  local _arg_decimal_stripped
  case "$_arg_decimal_value" in
    ''|*[!0-9]*)
      echo "$ARG_PROGRAM: $_arg_decimal_label must be a $_arg_decimal_range integer (got: $_arg_decimal_value)" >&2
      exit 2 ;;
  esac
  _arg_decimal_stripped=${_arg_decimal_value#"${_arg_decimal_value%%[!0]*}"}
  _arg_decimal_stripped=${_arg_decimal_stripped:-0}
  if [ "${#_arg_decimal_stripped}" -gt 10 ] || [ "$_arg_decimal_stripped" -gt 2147483647 ]; then
    echo "$ARG_PROGRAM: $_arg_decimal_label must be a $_arg_decimal_range integer (got: $_arg_decimal_value)" >&2
    exit 2
  fi
  if [ "$_arg_decimal_range" = "positive" ] && [ "$_arg_decimal_stripped" -eq 0 ]; then
    echo "$ARG_PROGRAM: $_arg_decimal_label must be a $_arg_decimal_range integer (got: $_arg_decimal_value)" >&2
    exit 2
  fi
  printf -v "$1" '%s' "$_arg_decimal_stripped"
}

# ---------------------------------------------------------------------------
# Spotlight marker
# ---------------------------------------------------------------------------
# A `.metadata_never_index` marker applies recursively, so marking a directory
# covers everything beneath it. Shared here so clean.sh and
# spotlight-exclude-artifacts cannot drift on filename or ownership.
#
# clean.sh marks inline rather than shelling out to spotlight-exclude-artifacts
# per directory: it already holds the full artifact list from discovery, and
# there are ~1000 of them, so a subprocess each would dominate the runtime.
# Sharing the function keeps the behaviour identical without the fork cost.
SPOTLIGHT_MARKER=".metadata_never_index"

# spotlight_mark DIR [OWNER]
#   0 = marked, 1 = already present, 2 = absent/unwritable
spotlight_mark() {
  local dir="$1" owner="${2:-}"
  [ -d "$dir" ] || return 2
  [ -e "$dir/$SPOTLIGHT_MARKER" ] && return 1
  touch "$dir/$SPOTLIGHT_MARKER" 2>/dev/null || return 2
  [ -n "$owner" ] && chown "$owner" "$dir/$SPOTLIGHT_MARKER" 2>/dev/null
  return 0
}
