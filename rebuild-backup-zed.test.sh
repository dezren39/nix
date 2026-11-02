#!/usr/bin/env bash
# Tests for the rebuild.sh / backup-zed.sh argument guards (#1190):
#   PR #1169 added the same no-argument guard to three scripts
#   (#1167), but full-clean.test.sh pins only the bash script. The
#   two zsh scripts' copies of the guard had no test at all, so a
#   future edit (or a zsh parsing quirk) could silently restore the
#   #1167 behaviour: --help / a typo'd flag runs the rebuild or the
#   backup and exits 0.
# This drives the REAL rebuild.sh and backup-zed.sh under zsh (their
# interpreter) in scratch dirs:
#   * rebuild.sh: git / nix are logging PATH stubs and
#     ./simple-rebuild.sh is a logging step stub beside the script.
#   * backup-zed.sh: the backup writes only inside the scratch work
#     dir from a fixture HOME, so its side effects are directly
#     observable (a copied settings.json / index.json / installed.txt
#     appears only when the script actually runs).
# It pins, for each script:
#   * --help and -h -> usage on stdout, exit 0, no side effects;
#   * any other argument -> exit 2, the argument named on stderr,
#     usage on stderr, no side effects;
#   * no argument -> the stubbed/fixture sequence runs and exits 0
#     (the control: the empty-log legs above pass only because the
#     guard stops the script, not because it cannot run).
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

BIN="$TMP_ROOT/bin"
HOME_DIR="$TMP_ROOT/home"
LOG="$TMP_ROOT/steps.log"
mkdir -p "$BIN" "$HOME_DIR"
: > "$LOG"

# PATH stubs for rebuild.sh: log every call, exit 0.
for cmd in git nix; do
  cat > "$BIN/$cmd" <<EOF
#!/bin/sh
echo "$cmd \$*" >> "$LOG"
exit 0
EOF
  chmod +x "$BIN/$cmd"
done
# rebuild.sh rewrites its path with HOME/.nix-profile/bin first, so
# the stubs must live there too or the system git/nix earlier in that
# list would win over $BIN in a non-sandbox run.
mkdir -p "$HOME_DIR/.nix-profile/bin"
cp "$BIN/git" "$BIN/nix" "$HOME_DIR/.nix-profile/bin/"

# --- rebuild.sh fixture -------------------------------------------------------
REBUILD_WORK="$TMP_ROOT/rebuild"
mkdir -p "$REBUILD_WORK"
cp "$SELF_DIR/rebuild.sh" "$REBUILD_WORK/rebuild.sh"
cat > "$REBUILD_WORK/simple-rebuild.sh" <<EOF
#!/bin/sh
echo "simple-rebuild.sh \$*" >> "$LOG"
exit 0
EOF
chmod +x "$REBUILD_WORK/simple-rebuild.sh"

# --- backup-zed.sh fixture ----------------------------------------------------
BACKUP_WORK="$TMP_ROOT/backup"
mkdir -p "$BACKUP_WORK/.zed"
cp "$SELF_DIR/backup-zed.sh" "$BACKUP_WORK/backup-zed.sh"
mkdir -p "$HOME_DIR/.config/zed" \
  "$HOME_DIR/Library/Application Support/Zed/extensions/installed/ext-a" \
  "$HOME_DIR/Library/Application Support/Zed/extensions/installed/ext-b"
echo '{"fixture": "settings"}' > "$HOME_DIR/.config/zed/settings.json"
echo '{"fixture": "index"}' \
  > "$HOME_DIR/Library/Application Support/Zed/extensions/index.json"

RC=0
run_script() { # run_script <workdir> <script> [args...] — sets RC; out/err in $TMP_ROOT
  local work="$1" script="$2"; shift 2
  HOME="$HOME_DIR" PATH="$BIN:$PATH" \
    zsh "$work/$script" "$@" > "$TMP_ROOT/out" 2> "$TMP_ROOT/err"
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

# --- rebuild.sh: --help / -h --------------------------------------------------
for arg in --help -h; do
  : > "$LOG"
  run_script "$REBUILD_WORK" rebuild.sh "$arg"
  expect_rc "rebuild.sh $arg exits 0" 0
  check "rebuild.sh $arg prints usage on stdout" \
    grep -q "Usage: rebuild.sh" "$TMP_ROOT/out"
  expect_no_steps "rebuild.sh $arg"
done

# --- rebuild.sh: unknown argument ---------------------------------------------
: > "$LOG"
run_script "$REBUILD_WORK" rebuild.sh --bogus
expect_rc "rebuild.sh --bogus exits 2" 2
check "rebuild.sh --bogus is named on stderr" \
  grep -q -- "--bogus" "$TMP_ROOT/err"
check "rebuild.sh --bogus prints usage on stderr" \
  grep -q "Usage: rebuild.sh" "$TMP_ROOT/err"
expect_no_steps "rebuild.sh --bogus"

# --- rebuild.sh: no-argument control ------------------------------------------
: > "$LOG"
run_script "$REBUILD_WORK" rebuild.sh
expect_rc "rebuild.sh no argument exits 0" 0
check "rebuild.sh no argument runs git add" grep -q "^git add \.$" "$LOG"
check "rebuild.sh no argument runs nix flake update" \
  grep -q "^nix .* flake update$" "$LOG"
check "rebuild.sh no argument runs ./simple-rebuild.sh" \
  grep -q "^simple-rebuild.sh" "$LOG"

# --- backup-zed.sh: --help / -h ------------------------------------------------
for arg in --help -h; do
  run_script "$BACKUP_WORK" backup-zed.sh "$arg"
  expect_rc "backup-zed.sh $arg exits 0" 0
  check "backup-zed.sh $arg prints usage on stdout" \
    grep -q "Usage: backup-zed.sh" "$TMP_ROOT/out"
  check "backup-zed.sh $arg copies no settings" \
    test ! -e "$BACKUP_WORK/.zed/settings.json"
done

# --- backup-zed.sh: unknown argument -------------------------------------------
run_script "$BACKUP_WORK" backup-zed.sh --bogus
expect_rc "backup-zed.sh --bogus exits 2" 2
check "backup-zed.sh --bogus is named on stderr" \
  grep -q -- "--bogus" "$TMP_ROOT/err"
check "backup-zed.sh --bogus prints usage on stderr" \
  grep -q "Usage: backup-zed.sh" "$TMP_ROOT/err"
check "backup-zed.sh --bogus copies no settings" \
  test ! -e "$BACKUP_WORK/.zed/settings.json"

# --- backup-zed.sh: no-argument control -----------------------------------------
run_script "$BACKUP_WORK" backup-zed.sh
expect_rc "backup-zed.sh no argument exits 0" 0
check "backup-zed.sh no argument copies settings" \
  grep -q '"fixture": "settings"' "$BACKUP_WORK/.zed/settings.json"
check "backup-zed.sh no argument copies the extensions index" \
  grep -q '"fixture": "index"' "$BACKUP_WORK/.zed/index.json"
check "backup-zed.sh no argument lists installed extensions sorted" \
  grep -qx "ext-a" "$BACKUP_WORK/.zed/installed.txt"
check "backup-zed.sh no argument lists ext-b" \
  grep -qx "ext-b" "$BACKUP_WORK/.zed/installed.txt"

if [ "$failures" -gt 0 ]; then
  echo "$failures failure(s)" >&2
  exit 1
fi
echo "all rebuild/backup-zed tests passed"
