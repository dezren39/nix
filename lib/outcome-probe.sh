#!/usr/bin/env bash
# SPDX-License-Identifier: MIT OR Apache-2.0
#
# outcome-probe.sh -- shared conformance matrix for the operation outcome
# contract (design #1339, phase 1).
#
# Sourced by a family test suite and driven through that suite's fail()
# function. Each row runs in an isolated shell so outcome_finish's exit is
# observable and parallel-worker state crosses only through fail files.

_outcome_probe_capture() {
  local _outcome_probe_dir="$1" _outcome_probe_status=0
  shift
  "$@" >"$_outcome_probe_dir/stdout" 2>"$_outcome_probe_dir/stderr" || \
    _outcome_probe_status=$?
  printf '%s\n' "$_outcome_probe_status" >"$_outcome_probe_dir/status"
}

_outcome_probe_ok() {
  local _outcome_probe_name="$1" _outcome_probe_status="$2"
  local _outcome_probe_stdout="$3" _outcome_probe_stderr="$4"
  if [ "$_outcome_probe_status" -eq 0 ] && [ -z "$_outcome_probe_stdout" ] && \
      [ -z "$_outcome_probe_stderr" ]; then
    echo "ok: outcome probe: $_outcome_probe_name"
  else
    fail "outcome probe: $_outcome_probe_name (status $_outcome_probe_status, stdout: $_outcome_probe_stdout, stderr: $_outcome_probe_stderr)"
  fi
}

# outcome_probe_matrix
#   Exercise failed sub-operation counting, enumeration status propagation,
#   silent success, parallel distinct-item dedupe, and unnamed worker death.
outcome_probe_matrix() {
  local _outcome_probe_lib _outcome_probe_root _outcome_probe_case
  local _outcome_probe_status _outcome_probe_stdout _outcome_probe_stderr
  _outcome_probe_lib="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"
  _outcome_probe_root="$(mktemp -d)" || { fail "outcome probe: could not create temp directory"; return; }

  # A failed sub-operation is named at its call site and summarized once.
  _outcome_probe_case="$_outcome_probe_root/failure"
  mkdir -p "$_outcome_probe_case"
  _outcome_probe_capture "$_outcome_probe_case" env \
    OUTCOME_COMMON="$_outcome_probe_lib" bash -c '
      . "$OUTCOME_COMMON"
      outcome_init outcome-probe
      outcome_fail item-a delete /fixture/item-a 7 "shimmed deletion failure"
      outcome_finish
    '
  _outcome_probe_status="$(cat "$_outcome_probe_case/status")"
  _outcome_probe_stdout="$(cat "$_outcome_probe_case/stdout")"
  _outcome_probe_stderr="$(cat "$_outcome_probe_case/stderr")"
  case "$_outcome_probe_stderr" in
    *"delete failed for /fixture/item-a (status 7)"*"outcome-probe: 1 operation(s) failed"*"/fixture/item-a"*)
      [ "$_outcome_probe_status" -eq 1 ] && [ -z "$_outcome_probe_stdout" ] && \
        echo "ok: outcome probe: failed sub-operation is counted and summarized" || \
        fail "outcome probe: failed sub-operation exited unexpectedly (status $_outcome_probe_status, stdout: $_outcome_probe_stdout)" ;;
    *) fail "outcome probe: failed sub-operation summary missing (stderr: $_outcome_probe_stderr)" ;;
  esac

  # An empty producer result remains distinguishable from a successful empty
  # enumeration: the status and stderr survive the process boundary.
  _outcome_probe_case="$_outcome_probe_root/enumeration"
  mkdir -p "$_outcome_probe_case"
  _outcome_probe_capture "$_outcome_probe_case" env \
    OUTCOME_COMMON="$_outcome_probe_lib" \
    OUTCOME_PROBE_CASE="$_outcome_probe_case" bash -c '
      . "$OUTCOME_COMMON"
      outcome_init outcome-probe
      outcome_enumerate "$OUTCOME_PROBE_CASE/status-file" "$OUTCOME_PROBE_CASE/stderr-file" -- \
        bash -c '\''printf "stub find failed\\n" >&2; exit 23'\''
      enum_status=$(cat "$OUTCOME_PROBE_CASE/status-file")
      enum_stderr=$(cat "$OUTCOME_PROBE_CASE/stderr-file")
      if [ "$enum_status" -ne 0 ]; then
        outcome_fail enumeration "find" /fixture "$enum_status" "$enum_stderr"
      fi
      outcome_finish
    '
  _outcome_probe_status="$(cat "$_outcome_probe_case/status")"
  _outcome_probe_stdout="$(cat "$_outcome_probe_case/stdout")"
  _outcome_probe_stderr="$(cat "$_outcome_probe_case/stderr")"
  case "$_outcome_probe_stderr" in
    *"find failed for /fixture (status 23): stub find failed"*"outcome-probe: 1 operation(s) failed"*)
      [ "$_outcome_probe_status" -eq 1 ] && [ -z "$_outcome_probe_stdout" ] && \
        echo "ok: outcome probe: failed enumeration cannot look like empty success" || \
        fail "outcome probe: failed enumeration exited unexpectedly (status $_outcome_probe_status)" ;;
    *) fail "outcome probe: enumeration status/stderr was lost (stderr: $_outcome_probe_stderr)" ;;
  esac

  # A successful run is silent and exits zero.
  _outcome_probe_case="$_outcome_probe_root/success"
  mkdir -p "$_outcome_probe_case"
  _outcome_probe_capture "$_outcome_probe_case" env OUTCOME_COMMON="$_outcome_probe_lib" bash -c '
    . "$OUTCOME_COMMON"
    outcome_init outcome-probe
    outcome_finish
  '
  _outcome_probe_status="$(cat "$_outcome_probe_case/status")"
  _outcome_probe_ok "successful run is silent" "$_outcome_probe_status" \
    "$(cat "$_outcome_probe_case/stdout")" "$(cat "$_outcome_probe_case/stderr")"

  # Two workers failing the same item produce one summary item, while a
  # separate item remains independently countable.
  _outcome_probe_case="$_outcome_probe_root/parallel"
  mkdir -p "$_outcome_probe_case"
  _outcome_probe_capture "$_outcome_probe_case" env \
    OUTCOME_COMMON="$_outcome_probe_lib" \
    OUTCOME_PROBE_CASE="$_outcome_probe_case" bash -c '
      . "$OUTCOME_COMMON"
      outcome_init outcome-probe
      outcome_fail_files "$OUTCOME_PROBE_CASE/failures"
      (
        . "$OUTCOME_COMMON"
        outcome_init outcome-probe
        outcome_fail item-a remove /fixture/item-a 1 "worker one"
      )
      (
        . "$OUTCOME_COMMON"
        outcome_init outcome-probe
        outcome_fail item-a remove /fixture/item-a 2 "worker two"
      )
      (
        . "$OUTCOME_COMMON"
        outcome_init outcome-probe
        outcome_fail item-b remove /fixture/item-b 1 "worker three"
      )
      outcome_finish
    '
  _outcome_probe_status="$(cat "$_outcome_probe_case/status")"
  _outcome_probe_stderr="$(grep 'operation(s) failed:' "$_outcome_probe_case/stderr")"
  case "$_outcome_probe_stderr" in
    *"outcome-probe: 2 operation(s) failed"*)
      case "$_outcome_probe_stderr" in
        *"/fixture/item-a"*"/fixture/item-b"*|*"/fixture/item-b"*"/fixture/item-a"*)
          [ "$_outcome_probe_status" -eq 1 ] && \
            echo "ok: outcome probe: parallel failures deduplicate by item" || \
            fail "outcome probe: parallel failures exited unexpectedly (status $_outcome_probe_status)" ;;
        *) fail "outcome probe: summary omitted a distinct item (stderr: $_outcome_probe_stderr)" ;;
      esac ;;
    *) fail "outcome probe: parallel failure count was wrong (stderr: $_outcome_probe_stderr)" ;;
  esac

  # An unnamed worker death uses a stable key, so multiple such deaths count
  # once even though neither worker could name its repo.
  _outcome_probe_case="$_outcome_probe_root/unnamed"
  mkdir -p "$_outcome_probe_case"
  _outcome_probe_capture "$_outcome_probe_case" env \
    OUTCOME_COMMON="$_outcome_probe_lib" \
    OUTCOME_PROBE_CASE="$_outcome_probe_case" bash -c '
      . "$OUTCOME_COMMON"
      outcome_init outcome-probe
      outcome_fail_files "$OUTCOME_PROBE_CASE/failures"
      (
        . "$OUTCOME_COMMON"
        outcome_init outcome-probe
        outcome_fail_unnamed xargs 123 "worker exited without naming a repo"
      )
      (
        . "$OUTCOME_COMMON"
        outcome_init outcome-probe
        outcome_fail_unnamed xargs 124 "worker exited without naming a repo"
      )
      outcome_finish
    '
  _outcome_probe_status="$(cat "$_outcome_probe_case/status")"
  _outcome_probe_stderr="$(cat "$_outcome_probe_case/stderr")"
  case "$_outcome_probe_stderr" in
    *"outcome-probe: 1 operation(s) failed"*"unknown item"*)
      [ "$_outcome_probe_status" -eq 1 ] && \
        echo "ok: outcome probe: unnamed worker death counts once" || \
        fail "outcome probe: unnamed worker failures exited unexpectedly (status $_outcome_probe_status)" ;;
    *) fail "outcome probe: unnamed worker death was not counted once (stderr: $_outcome_probe_stderr)" ;;
  esac

  rm -rf "$_outcome_probe_root"
}
