#!/usr/bin/env bash
# SPDX-License-Identifier: MIT OR Apache-2.0
#
# common.sh — shared library for clean.sh, git-maintain-repos,
# spotlight-exclude-artifacts and git-discover-repos.
#
# Sourced, never executed. Holds the three directory-name lists, the find(1)
# fragment builder, and the Spotlight marker helper -- everything more than one
# of those scripts needs to agree on.
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
