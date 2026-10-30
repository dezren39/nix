#!/usr/bin/env bash
# Tests for full-clean.sh (#1167):
#   full-clean.sh — the repo's most destructive wrapper (darwin
#   switch, opencode DB prune with --apply, vacuum, backup deletion,
#   sudo nix-collect-garbage -d) — had no argument parser at all:
#   --help, --bogus and --dry-run were silently ignored, the full
#   sequence ran, and it exited 0. rebuild.sh / backup-zed.sh share
#   the shape and get the same guard; this suite pins full-clean.sh.
# This drives the REAL full-clean.sh in a scratch dir where ./s,
# ./opencode-db-prune.sh and ./opencode-db-vacuum.sh are logging
# stubs, and sudo / nix-collect-garbage / nix / df are logging PATH
# stubs; pgrep is stubbed to "no opencode running". It pins:
#   * --help -> usage on stdout, exit 0, no stub called;
#   * --bogus -> exit 2, the argument named on stderr, usage on
#     stderr, no stub called;
#   * --dry-run -> the same exit-2 rejection (there is no dry-run
#     mode; the flag must not run the sequence);
#   * no argument -> the full stubbed sequence runs in order and
#     exits 0 (the control: the empty-log legs above pass only
#     because the guard stops the script, not because the stubs
#     are unreachable).
# Stubs use #!/bin/sh: the nix build sandbox has no /usr/bin/env.
set -uo pipefail

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

failures=0
fail() {
  echo "FAIL: $1" >&2
  failures=$((failures + 1))
}
check() { # check <description> <command...>
  local desc="$1"; shift
  if "$@" >/dev/null 2>&1; then
    echo "ok: $desc"
  else
    fail "$desc"
  fi
}

WORK="$TMP_ROOT/work"
BIN="$TMP_ROOT/bin"
HOME_DIR="$TMP_ROOT/home"
mkdir -p "$WORK" "$BIN" "$HOME_DIR"
LOG="$TMP_ROOT/steps.log"
: > "$LOG"

cp "$SELF_DIR/full-clean.sh" "$WORK/full-clean.sh"

# Step stubs beside the script: each logs its name + args and exits 0.
for step in s opencode-db-prune.sh opencode-db-vacuum.sh; do
  cat > "$WORK/$step" <<EOF
#!/bin/sh
echo "$step \$*" >> "$LOG"
exit 0
EOF
  chmod +x "$WORK/$step"
done

# PATH stubs: pgrep reports "none running" (exit 1, unlogged — it is
# a guard, not a step); the rest log every call.
cat > "$BIN/pgrep" <<'EOF'
#!/bin/sh
exit 1
EOF
for cmd in sudo nix-collect-garbage nix df; do
  cat > "$BIN/$cmd" <<EOF
#!/bin/sh
echo "$cmd \$*" >> "$LOG"
echo "stub $cmd output"
exit 0
EOF
  chmod +x "$BIN/$cmd"
done
chmod +x "$BIN/pgrep"

RC=0
run_full_clean() { # run_full_clean [args...] — sets RC; out/err under $TMP_ROOT
  : > "$LOG"
  HOME="$HOME_DIR" PATH="$BIN:$PATH" \
    bash "$WORK/full-clean.sh" "$@" > "$TMP_ROOT/out" 2> "$TMP_ROOT/err"
  RC=$?
}
expect_rc() { # expect_rc <desc> <want>
  if [ "$RC" -eq "$2" ]; then
    echo "ok: $1 (exit $2)"
  else
    fail "$1 — expected exit $2, got $RC; stderr: $(cat "$TMP_ROOT/err")"
  fi
}
expect_no_steps() { # expect_no_steps <desc>
  if [ ! -s "$LOG" ]; then
    echo "ok: $1 made no step calls"
  else
    fail "$1 made step calls — log: $(cat "$LOG")"
  fi
}

# --- --help: usage, exit 0, no steps -----------------------------------------
run_full_clean --help
expect_rc "--help exits 0" 0
check "--help prints usage on stdout" grep -q "Usage: full-clean.sh" "$TMP_ROOT/out"
expect_no_steps "--help"

# --- --bogus: exit 2, named, usage on stderr, no steps ------------------------
run_full_clean --bogus
expect_rc "unknown argument exits 2" 2
check "unknown argument is named on stderr" grep -q -- "--bogus" "$TMP_ROOT/err"
check "unknown argument prints usage on stderr" grep -q "Usage: full-clean.sh" "$TMP_ROOT/err"
expect_no_steps "--bogus"

# --- --dry-run: exit 2, named, no steps (there is no dry-run mode) ------------
run_full_clean --dry-run
expect_rc "--dry-run exits 2" 2
check "--dry-run is named on stderr" grep -q -- "--dry-run" "$TMP_ROOT/err"
expect_no_steps "--dry-run"

# --- no argument: the full stubbed sequence runs, exit 0 ----------------------
run_full_clean
expect_rc "no argument exits 0" 0
check "no argument runs ./s" grep -q "^s " "$LOG"
check "no argument runs the prune step" \
  grep -q "^opencode-db-prune.sh --protect-days 0 --apply -y$" "$LOG"
check "no argument runs the vacuum step" \
  grep -q "^opencode-db-vacuum.sh --auto-vacuum -y$" "$LOG"
check "no argument runs sudo GC" grep -q "^sudo nix-collect-garbage -d$" "$LOG"
check "no argument runs nix store optimise" grep -q "^nix store optimise$" "$LOG"

if [ "$failures" -gt 0 ]; then
  echo "$failures failure(s)" >&2
  exit 1
fi
echo "all full-clean tests passed"
