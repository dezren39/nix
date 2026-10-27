#!/usr/bin/env bash
# Tests for symlinker.sh's undo safety net:
#   #883 — BACKUP: undo records were colon-separated and split at the first
#          colon, so a conflict whose filename contains a colon could never
#          be restored (the original was stranded under its .bak.<ts> name
#          while undo exited 0). v3 records are tab-separated; v2 colon
#          records still parse for colon-free paths.
#   #889 — undo-of-undo files list RELINK before the swapped BACKUP record
#          that frees the link path, so in written order the relink always
#          failed "already exists" and the original was stranded under
#          .bak.<ts> with no symlink recreated. RELINK records are now
#          processed after the BACKUP restores.
#   #886 — RUN_TIMESTAMP has one-second resolution, so a run and its undo
#          started in the same second shared an id and the undo's
#          undo-of-undo header truncated the source undo file before it was
#          read ("0 unlinked", exit 0, records destroyed). Run ids now bump
#          to the next free digits-only value when artifacts already exist.
#   #912 — the name-filter flags' empty-occurrence guard tested the
#          cumulative FILTER_* array, so a repeated flag with no values
#          (--exact foo --exact) passed silently; the guard now compares
#          against the count before that occurrence consumed values.
# This drives the REAL symlinker.sh (no pasted copies) against temp dirs.
# The #886 scenario stubs `date` so every invocation lands in the same
# "second" deterministically.
set -uo pipefail

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$SELF_DIR/symlinker.sh"

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

undo_ts_of() { # undo_ts_of <cache-dir> — basename of the single .undo file
  local f
  f="$(ls "$1"/symlinker/*.undo 2>/dev/null | head -1)"
  [[ -n "$f" ]] && basename "$f" .undo
}

# ─── Scenario A (#883): colon + plain conflicts round-trip ──────────────────
A="$TMP_ROOT/a"; mkdir -p "$A/in" "$A/out" "$A/cache"
printf 'new\n' > "$A/in/a:b.txt";  printf 'old-colon\n' > "$A/out/a:b.txt"
printf 'new\n' > "$A/in/plain.txt"; printf 'old-plain\n' > "$A/out/plain.txt"

XDG_CACHE_HOME="$A/cache" bash "$SCRIPT" --input-dir "$A/in" --output-dir "$A/out" \
  --force --no-input >"$A/run.log" 2>&1 || fail "A: link run exited non-zero"
ts_a="$(undo_ts_of "$A/cache")"
[[ -n "$ts_a" ]] || fail "A: no undo file written"

# v3 BACKUP records are tab-separated (a literal tab between the two paths)
if grep -q $'^BACKUP:.*\t' "$A/cache/symlinker/$ts_a.undo" 2>/dev/null; then
  echo "ok: A: BACKUP records are tab-separated"
else
  fail "A: BACKUP records are not tab-separated"
fi

XDG_CACHE_HOME="$A/cache" bash "$SCRIPT" --undo "$ts_a" --no-input >"$A/undo.log" 2>&1 \
  || fail "A: undo of a fully restorable run exited non-zero"
check "A: colon file restored byte-for-byte" cmp -s "$A/out/a:b.txt" <(printf 'old-colon\n')
check "A: plain file restored byte-for-byte" cmp -s "$A/out/plain.txt" <(printf 'old-plain\n')
[[ ! -L "$A/out/a:b.txt" ]] || fail "A: colon file is still a symlink after undo"
[[ ! -e "$A/out/a:b.txt.bak.$ts_a" ]] || fail "A: colon backup left stranded after undo"

# ─── Scenario B (#883): legacy v2 colon-format undo file still parses ───────
B="$TMP_ROOT/b"; mkdir -p "$B/in" "$B/out" "$B/cache/symlinker"
printf 'new\n' > "$B/in/legacy.txt"
ln -s "$B/in/legacy.txt" "$B/out/legacy.txt"
printf 'old-legacy\n' > "$B/out/legacy.txt.bak.1600000000"
{
  echo "# UNDO_VERSION:2"
  echo "# symlinker undo — 1600000000"
  printf 'SYMLINK:%s\t%s\n' "$B/out/legacy.txt" "$B/in/legacy.txt"
  echo "BACKUP:$B/out/legacy.txt:$B/out/legacy.txt.bak.1600000000"
} > "$B/cache/symlinker/1600000000.undo"

XDG_CACHE_HOME="$B/cache" bash "$SCRIPT" --undo 1600000000 --no-input >"$B/undo.log" 2>&1 \
  || fail "B: undo of a legacy v2 file exited non-zero"
check "B: legacy colon-format backup restored" cmp -s "$B/out/legacy.txt" <(printf 'old-legacy\n')

# ─── Scenario C (#883): an irrecoverable undo exits non-zero ────────────────
C="$TMP_ROOT/c"; mkdir -p "$C/in" "$C/out" "$C/cache"
printf 'new\n' > "$C/in/gone.txt"; printf 'old-gone\n' > "$C/out/gone.txt"
XDG_CACHE_HOME="$C/cache" bash "$SCRIPT" --input-dir "$C/in" --output-dir "$C/out" \
  --force --no-input >"$C/run.log" 2>&1 || fail "C: link run exited non-zero"
ts_c="$(undo_ts_of "$C/cache")"
rm -f "$C/out/gone.txt.bak.$ts_c" # the backup is lost before the undo runs
if XDG_CACHE_HOME="$C/cache" bash "$SCRIPT" --undo "$ts_c" --no-input >"$C/undo.log" 2>&1; then
  fail "C: undo with an irrecoverable restore exited 0"
else
  echo "ok: C: undo with an irrecoverable restore exits non-zero"
fi
grep -q "irrecoverable" "$C/undo.log" || fail "C: undo did not report the irrecoverable restore"

# ─── Scenario D (#886): same-second runs/undo keep separate artifacts ───────
D="$TMP_ROOT/d"; mkdir -p "$D/bin" "$D/cache"
REAL_DATE="$(command -v date)"
{
  echo '#!/usr/bin/env bash'
  echo 'if [[ "$*" == "+%s" ]]; then echo 1700000000; else exec "'"$REAL_DATE"'" "$@"; fi'
} > "$D/bin/date"
chmod +x "$D/bin/date"
run_stubbed() { PATH="$D/bin:$PATH" XDG_CACHE_HOME="$D/cache" bash "$SCRIPT" "$@"; }

mkdir -p "$D/in1" "$D/out1" "$D/in2" "$D/out2"
printf 'new1\n' > "$D/in1/f.txt"; printf 'old1\n' > "$D/out1/f.txt"
printf 'new2\n' > "$D/in2/f.txt"; printf 'old2\n' > "$D/out2/f.txt"

run_stubbed --input-dir "$D/in1" --output-dir "$D/out1" --force --no-input >"$D/run1.log" 2>&1 \
  || fail "D: first run exited non-zero"
run_stubbed --input-dir "$D/in2" --output-dir "$D/out2" --force --no-input >"$D/run2.log" 2>&1 \
  || fail "D: second run exited non-zero"

check "D: first run kept its undo file" test -f "$D/cache/symlinker/1700000000.undo"
check "D: second run got the bumped id" test -f "$D/cache/symlinker/1700000001.undo"
grep -q "SYMLINK:$D/out1/f.txt" "$D/cache/symlinker/1700000000.undo" \
  || fail "D: first run's undo records were overwritten by the second run"
grep -q "SYMLINK:$D/out2/f.txt" "$D/cache/symlinker/1700000001.undo" \
  || fail "D: second run's undo records missing from its bumped undo file"

# The undo runs in the same "second" too: it must not truncate its source.
run_stubbed --undo 1700000000 --no-input >"$D/undo1.log" 2>&1 \
  || fail "D: same-second undo exited non-zero"
check "D: same-second undo restored the original" cmp -s "$D/out1/f.txt" <(printf 'old1\n')
[[ ! -L "$D/out1/f.txt" ]] || fail "D: same-second undo left the symlink in place"
grep -q "SYMLINK:$D/out1/f.txt" "$D/cache/symlinker/1700000000.undo" \
  || fail "D: same-second undo destroyed the source undo file"

# ─── Scenario E (#889): run → undo → undo-of-undo round-trip ───────────────
E="$TMP_ROOT/e"; mkdir -p "$E/in" "$E/out" "$E/cache"
printf 'new\n' > "$E/in/plain.txt"; printf 'old\n' > "$E/out/plain.txt"

XDG_CACHE_HOME="$E/cache" bash "$SCRIPT" --input-dir "$E/in" --output-dir "$E/out" \
  --force --no-input >"$E/run.log" 2>&1 || fail "E: link run exited non-zero"
ts_e="$(undo_ts_of "$E/cache")"
[[ -n "$ts_e" ]] || fail "E: no undo file written"
sleep 1

XDG_CACHE_HOME="$E/cache" bash "$SCRIPT" --undo "$ts_e" --no-input >"$E/undo.log" 2>&1 \
  || fail "E: undo exited non-zero"
check "E: undo restored the original" cmp -s "$E/out/plain.txt" <(printf 'old\n')
sleep 1

# The undo-of-undo file is the .undo file that is not the run's own.
ts_eu=""
for f in "$E/cache"/symlinker/*.undo; do
  b="$(basename "$f" .undo)"
  [[ "$b" != "$ts_e" ]] && ts_eu="$b"
done
[[ -n "$ts_eu" ]] || fail "E: no undo-of-undo file written"

XDG_CACHE_HOME="$E/cache" bash "$SCRIPT" --undo "$ts_eu" --no-input >"$E/undo2.log" 2>&1 \
  || fail "E: undo-of-undo of a --force run exited non-zero"
check "E: undo-of-undo recreated the symlink" test -L "$E/out/plain.txt"
if [[ -L "$E/out/plain.txt" && "$(readlink "$E/out/plain.txt")" == "$E/in/plain.txt" ]]; then
  echo "ok: E: recreated symlink points at the input file"
else
  fail "E: recreated symlink does not point at the input file"
fi
check "E: original left under its .bak.<run-ts> name" \
  cmp -s "$E/out/plain.txt.bak.$ts_e" <(printf 'old\n')

# ─── Scenario F (#912): repeated filter flag with no values errors ──────────
# The empty-occurrence guard used to test the cumulative FILTER_* array, so
# `--exact foo --exact` passed silently. Each occurrence must consume at
# least one value of its own.
F="$TMP_ROOT/f"; mkdir -p "$F/in" "$F/out" "$F/cache"
printf 'x\n' > "$F/in/foo"; printf 'x\n' > "$F/in/bar"
f_run() { XDG_CACHE_HOME="$F/cache" bash "$SCRIPT" --input-dir "$F/in" --output-dir "$F/out" --dry-run "$@" >/dev/null 2>"$F/err"; }
f_expect_error() { # f_expect_error <desc> <message> <args...>
  local desc="$1" msg="$2"; shift 2
  if f_run "$@"; then
    fail "F: $desc — expected exit 1, got 0"
  elif grep -qF -- "$msg" "$F/err"; then
    echo "ok: F: $desc"
  else
    fail "F: $desc — expected error '$msg', got: $(cat "$F/err")"
  fi
}
f_expect_error "repeated --exact with no values errors" "--exact requires at least one value" --exact foo --exact
f_expect_error "repeated --prefix with no values errors" "--prefix/--starts-with requires at least one value" --prefix f --prefix
f_expect_error "repeated --suffix with no values errors" "--suffix/--ends-with requires at least one value" --suffix o --suffix
f_expect_error "repeated --regex with no values errors" "--regex requires at least one value" --regex 'f.o' --regex
f_expect_error "repeated --match with no values errors" "--match requires at least one value" --match o --match --force
f_expect_error "first-use empty --exact still errors" "--exact requires at least one value" --exact
if f_run --exact foo --exact bar; then
  echo "ok: F: repeated --exact with values on both occurrences still passes"
else
  fail "F: repeated --exact with values on both occurrences should pass"
fi

if (( failures > 0 )); then
  echo "symlinker tests: $failures failure(s)" >&2
  exit 1
fi
echo "symlinker tests: all passed"
