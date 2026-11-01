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
#   #1092 — the type filter classified symlink targets with GNU-only
#          readlink -f; stock macOS readlink has no -f, its error was
#          swallowed, and --only-dirs silently dropped symlinks to
#          directories (exit 0). -f/-d tests follow symlinks portably;
#          scenario H pins both type filters, incl. a BSD-readlink shim.
#   #1126 — the value options --input-dir/--output-dir/--backup-suffix/
#          --undo assigned unconditionally, so a repeat silently last-won
#          and the first tree (or undo target) was never mentioned. They
#          now reject a repeat at parse time; accumulate flags stay
#          repeatable. Scenario I pins this.
#   #912 — the name-filter flags' empty-occurrence guard tested the
#          cumulative FILTER_* array, so a repeated flag with no values
#          (--exact foo --exact) passed silently; the guard now compares
#          against the count before that occurrence consumed values.
#   #1274 — --backup-suffix accepted a value containing '/', but the
#          suffix is spliced onto the conflict path, so a slash suffix
#          moved --force backups outside the output dir (and aimed the
#          --skip-backup rm -rf there too). It is now rejected at parse
#          time. Scenario K pins this.
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
  # The stub is executed by name via PATH (symlinker.sh's `date +%s`),
  # so its shebang must name an interpreter that exists in every
  # environment the suite runs in: /usr/bin/env does NOT exist inside
  # the nix build sandbox, which made this stub die with "bad
  # interpreter" there and failed all of Scenario D in the flake
  # check while the direct run passed (#1094). The resolved bash
  # path works in both (store bash in the sandbox, system bash out).
  echo "#!$(command -v bash)"
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

# ─── Scenario G (#930): flag-shaped values for value-taking options error ──
# --input-dir --dry-run used to swallow --dry-run as the input dir, so the
# preview flag was discarded. Every value-taking option must reject a
# following token starting with '-' in its missing-value error shape.
G="$TMP_ROOT/g"; mkdir -p "$G/in" "$G/out" "$G/cache"
printf 'x\n' > "$G/in/foo"
g_run() { XDG_CACHE_HOME="$G/cache" bash "$SCRIPT" "$@" >/dev/null 2>"$G/err"; }
g_expect_error() { # g_expect_error <desc> <message> <args...>
  local desc="$1" msg="$2"; shift 2
  if g_run "$@"; then
    fail "G: $desc — expected exit 1, got 0"
  elif grep -qF -- "$msg" "$G/err"; then
    echo "ok: G: $desc"
  else
    fail "G: $desc — expected error '$msg', got: $(cat "$G/err")"
  fi
}
g_expect_error "--input-dir --dry-run errors" "Error: --input-dir requires a value" --input-dir --dry-run
g_expect_error "--output-dir --force errors" "Error: --output-dir requires a value" --input-dir "$G/in" --output-dir --force
g_expect_error "--backup-suffix --dry-run errors" "Error: --backup-suffix requires a value" --input-dir "$G/in" --output-dir "$G/out" --backup-suffix --dry-run
g_expect_error "--undo --dry-run errors" "Error: --undo requires a timestamp" --input-dir "$G/in" --output-dir "$G/out" --undo --dry-run
g_expect_error "--exclude --dry-run errors" "Error: --exclude requires a value" --input-dir "$G/in" --output-dir "$G/out" --exclude --dry-run
check "G: error runs wrote no links" test -z "$(ls -A "$G/out")"
if g_run --input-dir "$G/in" --output-dir "$G/out" --dry-run; then
  echo "ok: G: valid values still parse (--dry-run run passes)"
else
  fail "G: valid --input-dir/--output-dir values should still pass"
fi


# ─── Scenario H (#1092): type filters classify symlinks portably ───────────
# Fixture: real dir, plain file, symlink-to-dir, symlink-to-file, broken
# symlink. --only-dirs must link realdir + linkdir; --only-files must link
# plain.txt + linkfile; the broken symlink matches neither. The same
# expectations must hold under a BSD-readlink shim (rejects -f), which is
# what stock macOS does to the old readlink -f classification.
H="$TMP_ROOT/h"; mkdir -p "$H/in/realdir" "$H/bin" "$H/cache"
printf 'x\n' > "$H/in/plain.txt"
ln -s "$H/in/realdir"   "$H/in/linkdir"
ln -s "$H/in/plain.txt" "$H/in/linkfile"
ln -s "$H/in/missing"   "$H/in/broken"
REAL_READLINK="$(command -v readlink)"
{
  echo "#!$(command -v bash)"
  echo 'for a in "$@"; do if [[ "$a" == "-f" ]]; then echo "readlink: illegal option -- f" >&2; exit 1; fi; done'
  echo 'exec "'"$REAL_READLINK"'" "$@"'
} > "$H/bin/readlink"
chmod +x "$H/bin/readlink"
h_run() { # h_run <outdir> <filter> [extra PATH prefix]
  local out="$1" filter="$2" prefix="${3:-}"
  mkdir -p "$out"
  if [[ -n "$prefix" ]]; then
    PATH="$prefix:$PATH" XDG_CACHE_HOME="$H/cache" bash "$SCRIPT" --input-dir "$H/in" --output-dir "$out" "$filter" --no-input >"$out.log" 2>&1
  else
    XDG_CACHE_HOME="$H/cache" bash "$SCRIPT" --input-dir "$H/in" --output-dir "$out" "$filter" --no-input >"$out.log" 2>&1
  fi
}
h_expect_dirs() { # h_expect_dirs <desc> <outdir>
  check "H: $1 links the real dir"   test -L "$2/realdir"
  check "H: $1 links symlink-to-dir" test -L "$2/linkdir"
  [[ ! -e "$2/plain.txt" && ! -L "$2/plain.txt" ]] || fail "H: $1 --only-dirs linked a plain file"
  [[ ! -e "$2/linkfile" && ! -L "$2/linkfile" ]] || fail "H: $1 --only-dirs linked a symlink-to-file"
  [[ ! -e "$2/broken" && ! -L "$2/broken" ]] || fail "H: $1 --only-dirs linked a broken symlink"
}
h_expect_files() { # h_expect_files <desc> <outdir>
  check "H: $1 links the plain file"   test -L "$2/plain.txt"
  check "H: $1 links symlink-to-file"  test -L "$2/linkfile"
  [[ ! -e "$2/realdir" && ! -L "$2/realdir" ]] || fail "H: $1 --only-files linked a real dir"
  [[ ! -e "$2/linkdir" && ! -L "$2/linkdir" ]] || fail "H: $1 --only-files linked a symlink-to-dir"
  [[ ! -e "$2/broken" && ! -L "$2/broken" ]] || fail "H: $1 --only-files linked a broken symlink"
}
h_run "$H/out-dirs" --only-dirs || fail "H: GNU --only-dirs run exited non-zero"
h_expect_dirs "GNU --only-dirs" "$H/out-dirs"
h_run "$H/out-dirs-bsd" --only-dirs "$H/bin" || fail "H: BSD-shim --only-dirs run exited non-zero"
h_expect_dirs "BSD-shim --only-dirs" "$H/out-dirs-bsd"
h_run "$H/out-files" --only-files || fail "H: GNU --only-files run exited non-zero"
h_expect_files "GNU --only-files" "$H/out-files"
h_run "$H/out-files-bsd" --only-files "$H/bin" || fail "H: BSD-shim --only-files run exited non-zero"
h_expect_files "BSD-shim --only-files" "$H/out-files-bsd"

# ─── Scenario I (#1126): repeated value options rejected at parse time ───
# --input-dir/--output-dir/--backup-suffix/--undo each assigned
# unconditionally, so a repeat silently discarded the first value: a
# duplicated --input-dir linked the second tree only, and a duplicated
# --undo undid the second run instead of the first. Each repeat must now
# fail at parse time (exit 1, naming the option and the only-once rule),
# before any linking or undo work; accumulate flags stay repeatable.
I="$TMP_ROOT/i"; mkdir -p "$I/in" "$I/in2" "$I/out" "$I/cache"
printf 'x\n' > "$I/in/foo"; printf 'y\n' > "$I/in2/bar"
i_run() { XDG_CACHE_HOME="$I/cache" bash "$SCRIPT" "$@" >/dev/null 2>"$I/err"; }
i_expect_error() { # i_expect_error <desc> <message> <args...>
  local desc="$1" msg="$2"; shift 2
  if i_run "$@"; then
    fail "I: $desc — expected exit 1, got 0"
  elif grep -qF -- "$msg" "$I/err"; then
    echo "ok: I: $desc"
  else
    fail "I: $desc — expected error '$msg', got: $(cat "$I/err")"
  fi
}
i_expect_error "repeated --input-dir errors" "Error: --input-dir may be given only once" --input-dir "$I/in" --input-dir "$I/in2" --output-dir "$I/out" --dry-run
i_expect_error "repeated --output-dir errors" "Error: --output-dir may be given only once" --input-dir "$I/in" --output-dir "$I/out" --output-dir "$I/out"
i_expect_error "repeated --backup-suffix errors" "Error: --backup-suffix may be given only once" --input-dir "$I/in" --output-dir "$I/out" --backup-suffix .bak.one --backup-suffix .bak.two
i_expect_error "repeated --undo errors" "Error: --undo may be given only once" --undo 1111111111 --undo 2222222222 --dry-run --no-input
check "I: rejected repeats wrote no links" test -z "$(ls -A "$I/out")"
if i_run --input-dir "$I/in" --output-dir "$I/out" --dry-run --exclude foo --exclude bar --exact foo --exact foo; then
  echo "ok: I: accumulate flags (--exclude/--exact) still repeat"
else
  fail "I: accumulate flags (--exclude/--exact) should still repeat: $(cat "$I/err")"
fi

# ─── Scenario J (#1220): empty multi-value filter values error ────────────
# --prefix "" used to count as a value for the #912 guard and then plan an
# unfiltered run. Every multi-value collector must reject an empty value
# at parse time naming the option.
J="$TMP_ROOT/j"; mkdir -p "$J/in" "$J/out" "$J/cache"
printf 'x\n' > "$J/in/foo"; printf 'x\n' > "$J/in/bar"
j_run() { XDG_CACHE_HOME="$J/cache" bash "$SCRIPT" --input-dir "$J/in" --output-dir "$J/out" --dry-run "$@" >/dev/null 2>"$J/err"; }
j_expect_error() { # j_expect_error <desc> <message> <args...>
  local desc="$1" msg="$2"; shift 2
  if j_run "$@"; then
    fail "J: $desc — expected exit 1, got 0"
  elif grep -qF -- "$msg" "$J/err"; then
    echo "ok: J: $desc"
  else
    fail "J: $desc — expected error '$msg', got: $(cat "$J/err")"
  fi
}
j_expect_error "empty --prefix errors" "--prefix/--starts-with requires a non-empty value" --prefix ''
j_expect_error "empty --starts-with errors" "--prefix/--starts-with requires a non-empty value" --starts-with ''
j_expect_error "empty --suffix errors" "--suffix/--ends-with requires a non-empty value" --suffix ''
j_expect_error "empty --ends-with errors" "--suffix/--ends-with requires a non-empty value" --ends-with ''
j_expect_error "empty --exact errors" "--exact requires a non-empty value" --exact ''
j_expect_error "empty --regex errors" "--regex requires a non-empty value" --regex ''
j_expect_error "empty --match errors" "--match requires a non-empty value" --match ''
j_expect_error "empty value after a real --prefix value errors" "--prefix/--starts-with requires a non-empty value" --prefix f ''
check "J: rejected empty filters wrote no links" test -z "$(ls -A "$J/out")"

# ─── Scenario K (#1274): slash in --backup-suffix rejected at parse time ──
# The suffix is concatenated onto the conflict path (bak="${lnk}<suffix>")
# and handed to mv (and, with --skip-backup, to rm -rf), so '/../x' used
# to move the backup outside the output dir at exit 0. Any '/' in the
# value must fail at parse time, before any work: error naming the
# option, exit 1, no symlink created, conflict untouched. A normal
# suffix (.old) must still back up alongside the link as today.
K="$TMP_ROOT/k"; mkdir -p "$K/in/tool" "$K/out/tool" "$K/cache"
printf 'original\n' > "$K/out/tool/keep.txt"
printf 'new\n' > "$K/in/tool/new.txt"
k_run() { XDG_CACHE_HOME="$K/cache" bash "$SCRIPT" --input-dir "$K/in" --output-dir "$K/out" --force --yes "$@" >"$K/log" 2>"$K/err"; }
if k_run --backup-suffix '/../x'; then
  fail "K: slash --backup-suffix should exit 1, got 0"
elif grep -qF "Error: --backup-suffix must not contain '/'" "$K/err"; then
  echo "ok: K: slash --backup-suffix rejected at parse time naming the option"
else
  fail "K: slash --backup-suffix — expected parse error naming --backup-suffix, got: $(cat "$K/err")"
fi
check "K: rejected run created no symlink" test ! -L "$K/out/tool"
check "K: rejected run left the conflict dir in place" test -d "$K/out/tool"
check "K: rejected run left conflict contents untouched" grep -qx original "$K/out/tool/keep.txt"
[[ ! -e "$TMP_ROOT/x" && ! -e "$K/x" ]] || fail "K: rejected run created an escaped backup path"
if k_run --backup-suffix '/../../escape/moved'; then
  fail "K: multi-step slash --backup-suffix should exit 1, got 0"
else
  echo "ok: K: multi-step slash --backup-suffix also rejected"
fi
check "K: conflict still untouched after second rejection" grep -qx original "$K/out/tool/keep.txt"
if k_run --backup-suffix .old; then
  echo "ok: K: normal --backup-suffix .old run passes"
else
  fail "K: normal --backup-suffix .old should pass: $(cat "$K/err") $(cat "$K/log")"
fi
check "K: normal suffix created the symlink" test -L "$K/out/tool"
check "K: normal suffix backed up alongside the link" test -d "$K/out/tool.old"
check "K: normal suffix backup kept the conflict contents" grep -qx original "$K/out/tool.old/keep.txt"

if (( failures > 0 )); then
  echo "symlinker tests: $failures failure(s)" >&2
  exit 1
fi
echo "symlinker tests: all passed"
