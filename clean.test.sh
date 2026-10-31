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
# Shared conformance probe matrix helpers (#1146, design #1145 phase 1).
# shellcheck source=lib/arg-probe.sh
. "$SELF_DIR/lib/arg-probe.sh"


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
OTHER="$TMP_ROOT/other"
mkdir -p "$OTHER"

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

# ─── Second / conflicting target is rejected at parse time (#1111) ────
# clean.sh used to assign every positional (and --path) to the single
# TARGET_PATH variable, so a second target silently discarded the first
# while the run exited 0 reporting success for a tree it never scanned.
# Every conflicting-target shape must exit 2 naming the unexpected
# argument, before any work runs (the snapshot check below covers the
# no-side-effects part for these runs too).
run --dry-run "$VALID" "$OTHER"
[ "$STATUS" -eq 2 ] || fail "second positional exited $STATUS (want 2)"
case "$ERR" in
  *"unexpected argument: $OTHER"*) echo "ok: second positional rejected, argument named" ;;
  *) fail "second positional did not name the argument on stderr (got: $ERR)" ;;
esac
run --dry-run --path "$VALID" "$OTHER"
[ "$STATUS" -eq 2 ] || fail "--path then positional exited $STATUS (want 2)"
case "$ERR" in
  *"unexpected argument: $OTHER"*) echo "ok: positional after --path rejected, argument named" ;;
  *) fail "positional after --path did not name the argument on stderr (got: $ERR)" ;;
esac
run --dry-run "$VALID" --path "$OTHER"
[ "$STATUS" -eq 2 ] || fail "positional then --path exited $STATUS (want 2)"
case "$ERR" in
  *"unexpected argument: --path"*) echo "ok: --path after positional rejected, option named" ;;
  *) fail "--path after positional did not name the option on stderr (got: $ERR)" ;;
esac
run --dry-run --path "$VALID" --path "$OTHER"
[ "$STATUS" -eq 2 ] || fail "repeated --path exited $STATUS (want 2)"
case "$ERR" in
  *"unexpected argument: --path"*) echo "ok: repeated --path rejected, option named" ;;
  *) fail "repeated --path did not name the option on stderr (got: $ERR)" ;;
esac

# --- Sibling conflict/repeat pairs are rejected at parse time (#1135) ---
# The 2026-10-29 batch guarded these pairs in the sibling scripts, but
# clean.sh still silently last-won them: --system/--no-system both wrote
# SCOPE_SET, --follow/--no-follow both wrote FOLLOW, and --depth /
# --docker-age assigned unconditionally. Every shape must exit 2 naming
# the conflict / only-once rule, mirroring git-maintain-repos /
# git-discover-repos.
for pair in "--system --no-system" "--no-system --system"; do
  # shellcheck disable=SC2086
  run --dry-run --path "$VALID" $pair
  [ "$STATUS" -eq 2 ] || fail "scope pair '$pair' exited $STATUS (want 2)"
  case "$ERR" in
    *"clean.sh: --system and --no-system may not be combined"*) echo "ok: scope pair '$pair' rejected as a conflict" ;;
    *) fail "scope pair '$pair' was not named as a conflict on stderr (got: $ERR)" ;;
  esac
done
for pair in "--follow --no-follow" "--no-follow --follow"; do
  # shellcheck disable=SC2086
  run --dry-run --path "$VALID" $pair
  [ "$STATUS" -eq 2 ] || fail "follow pair '$pair' exited $STATUS (want 2)"
  case "$ERR" in
    *"clean.sh: --follow and --no-follow may not be combined"*) echo "ok: follow pair '$pair' rejected as a conflict" ;;
    *) fail "follow pair '$pair' was not named as a conflict on stderr (got: $ERR)" ;;
  esac
done
for opt in --depth --docker-age; do
  run --dry-run --path "$VALID" "$opt" 1 "$opt" 2
  [ "$STATUS" -eq 2 ] || fail "repeated $opt exited $STATUS (want 2)"
  case "$ERR" in
    *"clean.sh: $opt may be given only once"*) echo "ok: repeated $opt rejected, option named" ;;
    *) fail "repeated $opt did not name the option on stderr (got: $ERR)" ;;
  esac
done
for flag in --system --no-system --follow --no-follow; do
  case "$flag" in
    --system) run "$flag" --only-unknown-scratch ;;
    *)        run --path "$VALID" "$flag" --only-unknown-scratch ;;
  esac
  [ "$STATUS" -eq 0 ] || fail "single $flag exited $STATUS (want 0, stderr: $ERR)"
  echo "ok: single $flag still accepted"
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
# Executed by name via PATH, so the shebang must name a real interpreter: /usr/bin/env does not exist in the nix build sandbox (#1099). Resolved bash works in both sandbox and direct runs.
echo "#!$(command -v bash)" > "$SHIM_DIR/stat"
cat >> "$SHIM_DIR/stat" <<'SHIM_EOF'
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

# ─── Shared conformance probe matrix (#1146, design #1145) ─────────
# The matrix rows for clean.sh, driven through lib/arg-probe.sh (the
# same helper every discovery suite uses): unknown, missing/flag-shaped
# (also pinned per-option above), repeat, both conflict orders, extra
# positional, and bad numbers -- abc, 0, oversized -- for both numeric
# options. 08 is probed as accepted decimal 8 (rule 6, #881).
probe_rejected "unknown option: --bogus" --bogus
probe_rejected "clean.sh: --depth requires a value" --depth
probe_rejected "clean.sh: --docker-age requires a value" --docker-age --dry-run
probe_rejected "clean.sh: --depth may be given only once" --dry-run --path "$VALID" --depth 1 --depth 2
probe_rejected "clean.sh: --system and --no-system may not be combined" --dry-run --system --no-system
probe_rejected "clean.sh: --system and --no-system may not be combined" --dry-run --no-system --system
probe_rejected "clean.sh: --follow and --no-follow may not be combined" --dry-run --path "$VALID" --follow --no-follow
probe_rejected "clean.sh: --follow and --no-follow may not be combined" --dry-run --path "$VALID" --no-follow --follow
probe_rejected "unexpected argument: $OTHER" --dry-run "$VALID" "$OTHER"
probe_rejected "clean.sh: empty positional argument (target must not be empty)" --dry-run ""
for bad in abc 0 2147483648 99999999999999999999999999; do
  probe_rejected "clean.sh: --depth must be a positive integer" --dry-run --path "$VALID" --depth "$bad"
  probe_rejected "clean.sh: --docker-age must be a positive integer" --dry-run --path "$VALID" --docker-age "$bad"
done
probe_accepted --path "$VALID" --depth 08 --docker-age 08 --only-unknown-scratch

[ "$failures" -eq 0 ] || { echo "$failures failure(s)" >&2; exit 1; }
echo "all clean.sh flag-shaped-value checks passed"
