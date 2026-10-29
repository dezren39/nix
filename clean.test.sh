#!/usr/bin/env bash
# Regression test for clean.sh's flag-shaped-value guards (#951).
#
# PR #939 (fix for #930) made clean.sh's value-taking options reject a
# flag-shaped next token — `--path --dry-run` used to swallow --dry-run
# as the path and run the cleanup for real — but added no test for
# clean.sh, so the guards could regress silently.
#
# The contract pinned here, for --path, --depth, and --docker-age:
#   <option> --dry-run  -> exit 2, "<option> requires a value" on
#                          stderr, and no side effects
#   valid values        -> accepted: the --only-unknown-scratch control
#                          run below parses all three options with real
#                          values and exits 0
#
# This drives the REAL clean.sh (no pasted copies) against temp dirs.
# The control uses --only-unknown-scratch so it exercises the parser and
# the post-parse judges, then sweeps only the isolated HOME's (empty)
# scratch root and exits — no docker/brew/git work runs.
set -uo pipefail

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$SELF_DIR/clean.sh"

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT
export HOME="$TMP_ROOT/home"
mkdir -p "$HOME"
# The control sweeps $HOME/.local/share/opencode/tmp; keep it absent so
# the sweep is a no-op regardless of the caller's real home.
unset DOCKER_AGE_DAYS DEPTH_LIMIT

failures=0
fail() {
  echo "FAIL: $1" >&2
  failures=$((failures + 1))
}

VALID="$TMP_ROOT/valid"
mkdir -p "$VALID"

run() { # run <args...> — sets OUT, ERR, STATUS
  OUT="$(bash "$SCRIPT" "$@" 2>"$TMP_ROOT/err")"; STATUS=$?
  ERR="$(cat "$TMP_ROOT/err")"
}

snapshot() { find "$VALID" "$HOME" | sort; }
BEFORE="$(snapshot)"

# ─── Flag-shaped values are rejected at parse time ─────────────────────
for opt in --path --depth --docker-age; do
  run "$opt" --dry-run
  [ "$STATUS" -eq 2 ] || fail "$opt --dry-run exited $STATUS (want 2)"
  case "$ERR" in
    *"clean.sh: $opt requires a value"*) echo "ok: $opt --dry-run rejected, option named" ;;
    *) fail "$opt --dry-run did not name the option on stderr (got: $ERR)" ;;
  esac
done

# ─── Missing values are rejected at parse time (#970) ──────────
# PR #921 made the value-taking options reject a missing value (option
# last, or an empty string) with exit 2, but no suite pinned that branch:
# a refactor dropping the -n check would pass the flag-shaped runs above
# while a trailing option silently took an empty value. Each option must
# exit 2 naming itself for both shapes.
for opt in --path --depth --docker-age; do
  run "$opt"
  [ "$STATUS" -eq 2 ] || fail "$opt (trailing, no value) exited $STATUS (want 2)"
  case "$ERR" in
    *"clean.sh: $opt requires a value"*) echo "ok: $opt trailing rejected, option named" ;;
    *) fail "$opt (trailing, no value) did not name the option on stderr (got: $ERR)" ;;
  esac
  run "$opt" ""
  [ "$STATUS" -eq 2 ] || fail "$opt (empty value) exited $STATUS (want 2)"
  case "$ERR" in
    *"clean.sh: $opt requires a value"*) echo "ok: $opt empty value rejected, option named" ;;
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
run --path "$VALID" --depth 2 --docker-age 7 --only-unknown-scratch
[ "$STATUS" -eq 0 ] || fail "valid-value control exited $STATUS (stderr: $ERR)"
case "$OUT" in
  *"nothing >=500MB"*) echo "ok: valid values parse and the control run exits 0" ;;
  *) fail "valid-value control output changed (got: $OUT)" ;;
esac

# ─── Portable stat: BSD-stat shim regression (#1068) ──────────────────
# clean.sh is a macOS tool, but three sites used GNU-only `stat -c`;
# BSD stat rejects -c, 2>/dev/null swallowed it, and the values came
# back empty: the pytest newest-run guard never fired and the
# unknown-scratch report always printed "unknown" dates. The shim
# below rejects -c exactly as macOS stat does and implements the BSD
# -f forms clean.sh's portable helpers use, translating them to the
# real (GNU) stat captured before the shim enters PATH. (The shim is
# test-only: using GNU date/stat internally to model BSD output is
# the point -- production clean.sh never sees it.)
REAL_STAT="$(command -v stat)"
SHIM_DIR="$TMP_ROOT/bsd-shim"
mkdir -p "$SHIM_DIR"
cat > "$SHIM_DIR/stat" <<'SHIM_EOF'
#!/usr/bin/env bash
[ -n "${REAL_STAT:-}" ] || exit 1
for _a in "$@"; do
  if [ "$_a" = "-c" ]; then echo "stat: illegal option -- c" >&2; exit 1; fi
done
_fmt=""; _tfmt=""; _file=""
while [ $# -gt 0 ]; do
  case "$1" in
    -f) _fmt="$2"; shift 2 ;;
    -t) _tfmt="$2"; shift 2 ;;
    *) _file="$1"; shift ;;
  esac
done
[ -n "$_fmt" ] && [ -n "$_file" ] || exit 1
case "$_fmt" in
  '%m') "$REAL_STAT" -c '%Y' "$_file" ;;
  '%u') "$REAL_STAT" -c '%u' "$_file" ;;
  '%SB'|'%Sm')
    if [ "$_fmt" = '%SB' ]; then
      _epoch="$("$REAL_STAT" -c '%W' "$_file" 2>/dev/null)"
      # Filesystems without btime report %W as 0; a BSD file always
      # has a birth time, so model it with the mtime there.
      case "$_epoch" in ''|*[!0-9]*|0) _epoch="$("$REAL_STAT" -c '%Y' "$_file")" ;; esac
    else
      _epoch="$("$REAL_STAT" -c '%Y' "$_file")"
    fi
    date -d "@$_epoch" +"${_tfmt:-%Y-%m-%d %H:%M:%S}" ;;
  *) echo "stat shim: unsupported -f format: $_fmt" >&2; exit 1 ;;
esac
SHIM_EOF
chmod +x "$SHIM_DIR/stat"

SCRATCH_BASE="$HOME/.local/share/opencode/tmp"
mkdir -p "$SCRATCH_BASE"

# Unknown-scratch report dates populate under BSD and GNU stat alike.
# Flagged-only mode (--only-unknown-scratch, no --prune): the fixture
# survives both runs, so the same entry serves the BSD and GNU passes.
UENTRY="$SCRATCH_BASE/zzscratch1068"
mkdir -p "$UENTRY"
dd if=/dev/zero of="$UENTRY/blob" bs=1024 count=1536 2>/dev/null
touch -d '2020-01-02 03:04:05' "$UENTRY"
for mode in bsd gnu; do
  if [ "$mode" = "bsd" ]; then
    OUT="$(REAL_STAT="$REAL_STAT" PATH="$SHIM_DIR:$PATH" UNKNOWN_SCRATCH_MIN_MB=1 UNKNOWN_SCRATCH_STALE_DAYS=1 bash "$SCRIPT" --only-unknown-scratch 2>"$TMP_ROOT/err")"; STATUS=$?
  else
    OUT="$(UNKNOWN_SCRATCH_MIN_MB=1 UNKNOWN_SCRATCH_STALE_DAYS=1 bash "$SCRIPT" --only-unknown-scratch 2>"$TMP_ROOT/err")"; STATUS=$?
  fi
  ERR="$(cat "$TMP_ROOT/err")"
  [ "$STATUS" -eq 0 ] || fail "unknown-scratch ($mode stat) exited $STATUS (stderr: $ERR)"
  ULINE="$(printf '%s\n' "$OUT" | grep 'zzscratch1068' || true)"
  [ -n "$ULINE" ] || { fail "unknown-scratch ($mode stat): fixture not reported"; continue; }
  case "$ULINE" in
    *"2020-01-02 03:04:05"*) echo "ok: unknown-scratch ($mode stat) report shows the real modified date" ;;
    *) fail "unknown-scratch ($mode stat): modified date not populated (line: $ULINE)" ;;
  esac
  case "$ULINE" in
    *unknown*) fail "unknown-scratch ($mode stat): report still shows unknown dates (line: $ULINE)" ;;
    *) echo "ok: unknown-scratch ($mode stat) report shows a created date" ;;
  esac
done

# Pytest newest-run guard fires under BSD and GNU stat alike. Both
# runs are older than PYTEST_KEEP_HOURS, so only the guard keeps the
# newest (pytest-2). --dry-run makes del() report instead of remove:
# under the #1068 bug pytest-2 appears in the would-delete list.
PTDIR="$SCRATCH_BASE/pytest-of-fixture"
mkdir -p "$PTDIR/pytest-1" "$PTDIR/pytest-2"
touch -d '10 hours ago' "$PTDIR/pytest-1"
touch -d '6 hours ago' "$PTDIR/pytest-2"
ln -s pytest-2 "$PTDIR/pytest-current"
for mode in bsd gnu; do
  if [ "$mode" = "bsd" ]; then
    OUT="$(REAL_STAT="$REAL_STAT" PATH="$SHIM_DIR:$PATH" PYTEST_KEEP_HOURS=4 TMPDIR="$SCRATCH_BASE" bash "$SCRIPT" --system --dry-run 2>"$TMP_ROOT/err")"; STATUS=$?
  else
    OUT="$(PYTEST_KEEP_HOURS=4 TMPDIR="$SCRATCH_BASE" bash "$SCRIPT" --system --dry-run 2>"$TMP_ROOT/err")"; STATUS=$?
  fi
  ERR="$(cat "$TMP_ROOT/err")"
  [ "$STATUS" -eq 0 ] || fail "pytest guard ($mode stat): --system --dry-run exited $STATUS (stderr: $ERR)"
  case "$OUT" in
    *"would delete:"*pytest-1*) echo "ok: pytest guard ($mode stat) schedules the older run for deletion" ;;
    *) fail "pytest guard ($mode stat): older pytest-1 not scheduled for deletion" ;;
  esac
  case "$OUT" in
    *"would delete:"*pytest-2*) fail "pytest guard ($mode stat): newest pytest-2 scheduled for deletion -- guard did not fire" ;;
    *) echo "ok: pytest guard ($mode stat) preserves the newest run" ;;
  esac
  [ -e "$PTDIR/pytest-current" ] || fail "pytest guard ($mode stat): pytest-current dangling after the run"
done

[ "$failures" -eq 0 ] || { echo "$failures failure(s)" >&2; exit 1; }
echo "all clean.sh flag-shaped-value checks passed"
