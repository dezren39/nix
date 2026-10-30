#!/usr/bin/env bash
# SPDX-License-Identifier: MIT OR Apache-2.0
#
# arg-probe.sh -- shared conformance probe matrix for the argument
# contract (design #1145, phase 1 / #1146).
#
# Sourced, never executed. Each of the four discovery suites
# (clean.test.sh, git-discover-repos.test.sh, git-maintain-repos.test.sh,
# spotlight-exclude-artifacts.test.sh) sources this file and drives its
# own script through the same matrix rows with these two probes, on top
# of the suite's own run()/fail() harness (run sets OUT, ERR, STATUS;
# fail records a failure):
#
#   probe_rejected <stderr-substring> <args...>
#     The invocation must exit 2 (the contract exit for a bad argument,
#     never 0 or a mid-run 1) and name the option/variable on stderr.
#     Parse-time rejection is also the no-side-effect guarantee: the
#     suites run these probes against their fixture trees and check the
#     trees afterwards, as their earlier sections do.
#   probe_accepted <args...>
#     The invocation must exit 0 (a valid invocation the contract must
#     not disturb).
#
# The matrix rows every family member drives (with its own options):
#   unknown option; missing value; flag-shaped value; empty value;
#   repeated single-value option; both orders of each conflict pair;
#   extra positional (where the script takes at most one target);
#   bad numbers for each numeric option -- abc, 0 where positive is
#   required, oversized (> INT_MAX, and > 64-bit) -- plus the
#   leading-zero row: 08 is strict plain decimal 8 (rule 6: a leading
#   zero is data, never octal, #881), so it is probed as ACCEPTED where
#   8 is in range, not as a bad number.

probe_rejected() {
  local _probe_want="$1"; shift
  run "$@"
  if [ "$STATUS" -ne 2 ]; then
    fail "probe: $* exited $STATUS (want 2)"
    return
  fi
  case "$ERR" in
    *"$_probe_want"*) echo "ok: probe: $* rejected (stderr names the option)" ;;
    *) fail "probe: $* stderr did not name the expected substring (got: $ERR)" ;;
  esac
}

probe_accepted() {
  run "$@"
  if [ "$STATUS" -ne 0 ]; then
    fail "probe: $* exited $STATUS (want 0, stderr: $ERR)"
    return
  fi
  echo "ok: probe: $* accepted"
}
