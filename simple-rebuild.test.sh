#!/usr/bin/env bash
# Regression test for simple-rebuild.sh's mode-flag parser (#1149).
#
# --build-only and --activate-only are documented alternatives
# ([--build-only|--activate-only [GENERATION]]) but the parser assigned
# mode unconditionally, so both together silently last-won:
# `--build-only --activate-only` ran the sudo activation phase, the
# reverse silently dropped to a build, a repeated --activate-only
# silently discarded the first GENERATION, and
# `--activate-only 5 --build-only` stranded the generation in a build
# run, exit 0. The build/activate split is a privilege boundary
# (no sudo vs sudo), so the parser now rejects all of those at parse
# time with exit 2 naming the flags.
#
# This suite cannot drive the real script end to end (a valid control
# would run a real nix build / darwin activation), so it extracts the
# REAL parser segment from simple-rebuild.sh -- from `mode="all"`
# through the GENERATION validation, stopping before the marker-commit
# helper -- and runs that segment under zsh, the script's interpreter.
# Nothing is a pasted copy: if the shipped parser changes, the
# extracted segment changes with it, and a missing marker fails here.
#
# Run: ./simple-rebuild.test.sh
# It is run by the flake check `simple-rebuild-tests`.
set -uo pipefail

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$SELF_DIR/simple-rebuild.sh"

command -v zsh >/dev/null || { echo "zsh required" >&2; exit 1; }

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

failures=0
fail() { echo "FAIL: $1" >&2; failures=$((failures + 1)); }
ok() { echo "ok: $1"; }

# Extract the parser segment from the real script.
SEGMENT="$TMP_ROOT/parser.zsh"
awk '/^mode="all"/{f=1} /^# Every marker commit/{f=0} f' "$SCRIPT" >"$SEGMENT"
[ -s "$SEGMENT" ] || { echo "could not extract parser segment from simple-rebuild.sh" >&2; exit 1; }
grep -q 'seen_activate' "$SEGMENT" || fail "extracted segment lacks the #1149 mode-flag guards"

PROBE="$TMP_ROOT/probe.zsh"
cat >"$PROBE" <<EOF
#!/usr/bin/env zsh
source "$SEGMENT" "\$@"
echo "mode=\$mode generation=\$generation no_commit=\$no_commit"
EOF

run() { # run <args...> -- sets OUT, ERR, STATUS
  OUT="$(zsh "$PROBE" "$@" 2>"$TMP_ROOT/err")"; STATUS=$?
  ERR="$(cat "$TMP_ROOT/err")"
}

expect_reject() { # expect_reject <label> <needle1> <needle2> <args...>
  local label="$1" n1="$2" n2="$3"; shift 3
  run "$@"
  [ "$STATUS" -eq 2 ] || { fail "$label exited $STATUS (want 2)"; return; }
  case "$ERR" in *"$n1"*) : ;; *) fail "$label stderr did not name $n1 (got: $ERR)"; return ;; esac
  if [ -n "$n2" ]; then
    case "$ERR" in *"$n2"*) : ;; *) fail "$label stderr did not name $n2 (got: $ERR)"; return ;; esac
  fi
  ok "$label rejected, exit 2, flags named"
}

expect_parse() { # expect_parse <label> <want mode=...> <args...>
  local label="$1" want="$2"; shift 2
  run "$@"
  [ "$STATUS" -eq 0 ] || { fail "$label exited $STATUS (want 0; err: $ERR)"; return; }
  [ "$OUT" = "$want" ] || { fail "$label parsed as '$OUT' (want '$want')"; return; }
  ok "$label parses as $want"
}

# --- Conflicting / repeated mode flags are rejected at parse time ---
expect_reject "--build-only --activate-only" "--build-only" "--activate-only" --build-only --activate-only
expect_reject "--activate-only --build-only" "--activate-only" "--build-only" --activate-only --build-only
expect_reject "--build-only --activate-only 5" "--build-only" "--activate-only" --build-only --activate-only 5
expect_reject "--activate-only 5 --build-only (stranded generation)" "--activate-only" "--build-only" --activate-only 5 --build-only
expect_reject "--activate-only 5 --activate-only 7" "--activate-only" "" --activate-only 5 --activate-only 7
expect_reject "--activate-only --activate-only" "--activate-only" "" --activate-only --activate-only
expect_reject "--no-commit --build-only --activate-only" "--build-only" "--activate-only" --no-commit --build-only --activate-only

# --- Valid invocations parse exactly as before ----------------------
expect_parse "no flags" "mode=all generation= no_commit=0"
expect_parse "--build-only" "mode=build generation= no_commit=0" --build-only
expect_parse "--activate-only" "mode=activate generation= no_commit=0" --activate-only
expect_parse "--activate-only 5" "mode=activate generation=5 no_commit=0" --activate-only 5
expect_parse "--no-commit" "mode=all generation= no_commit=1" --no-commit
expect_parse "--no-commit --build-only" "mode=build generation= no_commit=1" --no-commit --build-only
expect_parse "--build-only --no-commit" "mode=build generation= no_commit=1" --build-only --no-commit
expect_parse "--activate-only 5 --no-commit" "mode=activate generation=5 no_commit=1" --activate-only 5 --no-commit

# --- Pre-existing parser behaviour is intact ------------------------
# GENERATION validation (#1022): non-numeric still exits 2, and
# leading zeros are still normalised away.
expect_reject "--activate-only abc" "GENERATION" "" --activate-only abc
expect_parse "--activate-only 007" "mode=activate generation=7 no_commit=0" --activate-only 007
expect_reject "--bogus" "usage:" "" --bogus

if [ "$failures" -gt 0 ]; then
  echo "$failures simple-rebuild parser check(s) failed" >&2
  exit 1
fi
echo "all simple-rebuild parser checks passed"
