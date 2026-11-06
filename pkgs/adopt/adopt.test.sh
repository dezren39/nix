#!/usr/bin/env bash
# Tests for adopt.sh: it edits a scratch copy of the flake's hosts map, with
# scutil stubbed to a fixed LocalHostName. Scenarios:
#   A  default template adds the current host; a second run changes nothing
#   B  an existing host as template is copied under the new name
#   C  a template file becomes the entry body
#   D  --dry-run changes nothing; an unknown template fails, changing nothing
#   E  everything also works under a BWK-strict awk shim (#1074: macOS awk
#      rejects literal newlines in -v assignments; GNU/mawk do not)
#   F  one-line host entries (NAME = {};) are recognized (#1079): re-adopting
#      one reports already-a-host and copying one as a template works
#   G  a second positional TEMPLATE is rejected with exit 2 (#1080)
#   H  a repeated --host or --flake is rejected with exit 2 (#1103)
#   I  an empty --host or --flake value is rejected with exit 2 (#1212)
#   J  one-line entries ending in a trailing comment are recognized as
#      ended (#1269): copying one copies exactly that entry, even as last host
#   K  a template file whose read fails (sed stub exits 1) fails loudly:
#      non-zero exit naming the template, flake.nix unchanged (#1361)
#   L  a symlinked --flake is written through to its target (#1557):
#      entry lands in the target, the symlink stays a symlink, and the
#      target's mode is preserved
#   M  a non-644 regular flake keeps its mode after an edit (#1557)
#   N  a symlinked --flake with an ABSOLUTE target is written through
#      (#1561: scenario L covered only the relative-target branch of
#      the chain loop in adopt.sh)
#   O  a two-hop symlink chain resolves to the real flake (#1561)
set -uo pipefail

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$SELF_DIR/adopt.sh"

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

failures=0
fail() { echo "FAIL: $1" >&2; failures=$((failures + 1)); }
ok() { echo "ok: $1"; }

STUB_BIN="$TMP_ROOT/bin"
mkdir -p "$STUB_BIN"
cat > "$STUB_BIN/scutil" <<'EOS'
#!/bin/sh
# scutil --get LocalHostName (stub shebang is /bin/sh: the nix build
# sandbox has no /usr/bin/env — same convention as search.test.sh)
echo TESTMAC1
EOS
chmod +x "$STUB_BIN/scutil"
export PATH="$STUB_BIN:$PATH"

new_flake() {
  # $1 = directory to create the scratch flake.nix in
  mkdir -p "$1"
  cat > "$1/flake.nix" <<'EOF'
{
  outputs = { ... }: {
    hosts = {
      MGM9JJ4V3R = {
        # inherits default system "aarch64-darwin"
        extraModules = [ ./gpu.nix ];
      };
    };
  };
}
EOF
}

# A: default template, then idempotency
new_flake "$TMP_ROOT/a"
(cd "$TMP_ROOT/a" && bash "$SCRIPT" >/dev/null) || fail "A: adopt exited non-zero"
grep -q '^      TESTMAC1 = {$' "$TMP_ROOT/a/flake.nix" && ok "A: TESTMAC1 added" || fail "A: TESTMAC1 missing"
grep -q 'inherits default system' "$TMP_ROOT/a/flake.nix" && ok "A: default body" || fail "A: default body missing"
cp "$TMP_ROOT/a/flake.nix" "$TMP_ROOT/a/before"
out="$(cd "$TMP_ROOT/a" && bash "$SCRIPT")" || fail "A: second run exited non-zero"
[[ "$out" == *"already a host"* ]] && ok "A: second run reports already adopted" || fail "A: second run did not report already adopted"
diff -q "$TMP_ROOT/a/before" "$TMP_ROOT/a/flake.nix" >/dev/null && ok "A: second run changed nothing" || fail "A: second run changed the flake"

# B: copy an existing host
new_flake "$TMP_ROOT/b"
(cd "$TMP_ROOT/b" && bash "$SCRIPT" MGM9JJ4V3R --host NEWMAC >/dev/null) || fail "B: adopt exited non-zero"
grep -q '^      NEWMAC = {$' "$TMP_ROOT/b/flake.nix" && ok "B: NEWMAC added" || fail "B: NEWMAC missing"
awk '/^      NEWMAC = \{$/,/^      \};$/' "$TMP_ROOT/b/flake.nix" | grep -q 'extraModules = \[ ./gpu.nix \];' \
  && ok "B: template body copied" || fail "B: template body not copied"

# C: template file
new_flake "$TMP_ROOT/c"
printf 'system = "x86_64-darwin";\n' > "$TMP_ROOT/body.nix"
(cd "$TMP_ROOT/c" && bash "$SCRIPT" "$TMP_ROOT/body.nix" >/dev/null) || fail "C: adopt exited non-zero"
awk '/^      TESTMAC1 = \{$/,/^      \};$/' "$TMP_ROOT/c/flake.nix" | grep -q 'system = "x86_64-darwin";' \
  && ok "C: file body embedded" || fail "C: file body missing"

# D: dry-run + unknown template
new_flake "$TMP_ROOT/d"
cp "$TMP_ROOT/d/flake.nix" "$TMP_ROOT/d/before"
(cd "$TMP_ROOT/d" && bash "$SCRIPT" --dry-run >/dev/null) || fail "D: dry-run exited non-zero"
diff -q "$TMP_ROOT/d/before" "$TMP_ROOT/d/flake.nix" >/dev/null && ok "D: dry-run changed nothing" || fail "D: dry-run changed the flake"
(cd "$TMP_ROOT/d" && bash "$SCRIPT" NOSUCHHOST >/dev/null 2>&1) && fail "D: unknown template exited 0" || ok "D: unknown template fails"
diff -q "$TMP_ROOT/d/before" "$TMP_ROOT/d/flake.nix" >/dev/null && ok "D: failed run changed nothing" || fail "D: failed run changed the flake"

# E: BWK (macOS) awk strictness (#1074): macOS awk rejects a literal newline
# in a -v assignment ("newline in string"); GNU awk/mawk accept it, so the
# original multiline -v insertion passed on Linux and died on a Mac. This
# shim enforces the BWK rule on any awk, then delegates to the real one.
REAL_AWK="$(command -v awk)"
mkdir -p "$TMP_ROOT/bwk-bin"
cat > "$TMP_ROOT/bwk-bin/awk" <<EOS
#!/bin/sh
# Stub shebang is /bin/sh (POSIX form of the bash original): the nix
# build sandbox has no /usr/bin/env — same convention as search.test.sh.
prev=""
# NL below is a literal newline: a command substitution would strip it
# and leave NL empty, which would then match every -v argument.
NL="
"
for arg in "\$@"; do
  if [ "\$prev" = -v ]; then
    case "\$arg" in
      *"\$NL"*)
        echo "awk: newline in string" >&2
        exit 2
        ;;
    esac
  fi
  prev="\$arg"
done
exec "$REAL_AWK" "\$@"
EOS
chmod +x "$TMP_ROOT/bwk-bin/awk"
new_flake "$TMP_ROOT/e"
(cd "$TMP_ROOT/e" && PATH="$TMP_ROOT/bwk-bin:$STUB_BIN:$PATH" bash "$SCRIPT" >/dev/null) \
  || fail "E: adopt failed under BWK-strict awk"
grep -q '^      TESTMAC1 = {$' "$TMP_ROOT/e/flake.nix" \
  && ok "E: TESTMAC1 added under BWK-strict awk" || fail "E: TESTMAC1 missing under BWK-strict awk"
(cd "$TMP_ROOT/e" && PATH="$TMP_ROOT/bwk-bin:$STUB_BIN:$PATH" bash "$SCRIPT" MGM9JJ4V3R --host COPYMAC >/dev/null) \
  || fail "E: template copy failed under BWK-strict awk"
grep -q '^      COPYMAC = {$' "$TMP_ROOT/e/flake.nix" \
  && ok "E: template copy works under BWK-strict awk" || fail "E: COPYMAC missing under BWK-strict awk"

# F: one-line host entries (#1079)
mkdir -p "$TMP_ROOT/f"
cat > "$TMP_ROOT/f/flake.nix" <<'EOF'
{
  outputs = { ... }: {
    hosts = {
      ONELINE = {};
      BODYLINE = { system = "x86_64-darwin"; };
      MGM9JJ4V3R = {
        # inherits default system "aarch64-darwin"
        extraModules = [ ./gpu.nix ];
      };
    };
  };
}
EOF
cp "$TMP_ROOT/f/flake.nix" "$TMP_ROOT/f/before"
out="$(cd "$TMP_ROOT/f" && bash "$SCRIPT" --host ONELINE)" || fail "F: re-adopting a one-line host exited non-zero"
[[ "$out" == *"already a host"* ]] && ok "F: one-line host reports already adopted" || fail "F: one-line host did not report already adopted"
diff -q "$TMP_ROOT/f/before" "$TMP_ROOT/f/flake.nix" >/dev/null && ok "F: re-adopting a one-line host changed nothing" || fail "F: re-adopting a one-line host changed the flake"
(cd "$TMP_ROOT/f" && bash "$SCRIPT" ONELINE --host NEWMAC >/dev/null) || fail "F: copying a one-line host exited non-zero"
grep -q '^      NEWMAC = {};$' "$TMP_ROOT/f/flake.nix" && ok "F: one-line entry copied" || fail "F: one-line entry copy missing"
(cd "$TMP_ROOT/f" && bash "$SCRIPT" BODYLINE --host COPYMAC >/dev/null) || fail "F: copying a one-line host with a body exited non-zero"
grep -q '^      COPYMAC = { system = "x86_64-darwin"; };$' "$TMP_ROOT/f/flake.nix" && ok "F: one-line entry with body copied" || fail "F: one-line entry with body copy missing"
out="$(cd "$TMP_ROOT/f" && bash "$SCRIPT" NOSUCHHOST --host OTHERMAC 2>&1)" && fail "F: unknown template exited 0" || ok "F: unknown template fails"
[[ "$out" == *"ONELINE"* ]] && ok "F: error lists one-line hosts as existing" || fail "F: error omits one-line hosts from existing list"


# G: a second positional TEMPLATE is rejected (#1080)
new_flake "$TMP_ROOT/g"
cp "$TMP_ROOT/g/flake.nix" "$TMP_ROOT/g/before"
out="$(cd "$TMP_ROOT/g" && bash "$SCRIPT" MGM9JJ4V3R default --host TWOMAC 2>&1)"; rc=$?
(( rc == 2 )) && ok "G: two positionals exit 2" || fail "G: two positionals exited $rc, want 2"
[[ "$out" == *"only one TEMPLATE"* ]] && ok "G: error names the one-TEMPLATE rule" || fail "G: error does not name the one-TEMPLATE rule"
diff -q "$TMP_ROOT/g/before" "$TMP_ROOT/g/flake.nix" >/dev/null && ok "G: rejected run changed nothing" || fail "G: rejected run changed the flake"
out="$(cd "$TMP_ROOT/g" && bash "$SCRIPT" default MGM9JJ4V3R --host TWOMAC 2>&1)"; rc=$?
(( rc == 2 )) && ok "G: two positionals (default first) exit 2" || fail "G: two positionals (default first) exited $rc, want 2"
diff -q "$TMP_ROOT/g/before" "$TMP_ROOT/g/flake.nix" >/dev/null && ok "G: second rejected run changed nothing" || fail "G: second rejected run changed the flake"

# H: a repeated --host or --flake is rejected (#1103)
new_flake "$TMP_ROOT/h"
cp "$TMP_ROOT/h/flake.nix" "$TMP_ROOT/h/before"
out="$(cd "$TMP_ROOT/h" && bash "$SCRIPT" --host FIRST --host SECOND --dry-run 2>&1)"; rc=$?
(( rc == 2 )) && ok "H: repeated --host exits 2" || fail "H: repeated --host exited $rc, want 2"
[[ "$out" == *"--host may be given only once"* ]] && ok "H: error names the only-once rule for --host" || fail "H: error does not name the only-once rule for --host"
diff -q "$TMP_ROOT/h/before" "$TMP_ROOT/h/flake.nix" >/dev/null && ok "H: rejected --host run changed nothing" || fail "H: rejected --host run changed the flake"
out="$(cd "$TMP_ROOT/h" && bash "$SCRIPT" --flake ./flake.nix --flake ./other.nix --host NEWFLAKE 2>&1)"; rc=$?
(( rc == 2 )) && ok "H: repeated --flake exits 2" || fail "H: repeated --flake exited $rc, want 2"
[[ "$out" == *"--flake may be given only once"* ]] && ok "H: error names the only-once rule for --flake" || fail "H: error does not name the only-once rule for --flake"
diff -q "$TMP_ROOT/h/before" "$TMP_ROOT/h/flake.nix" >/dev/null && ok "H: rejected --flake run changed nothing" || fail "H: rejected --flake run changed the flake"
(cd "$TMP_ROOT/h" && bash "$SCRIPT" --host SINGLE --flake ./flake.nix --dry-run >/dev/null) && ok "H: single --host/--flake still work" || fail "H: single --host/--flake exited non-zero"

# I: an empty --host or --flake value is rejected (#1212)
new_flake "$TMP_ROOT/i"
cp "$TMP_ROOT/i/flake.nix" "$TMP_ROOT/i/before"
out="$(cd "$TMP_ROOT/i" && bash "$SCRIPT" --host "" --dry-run 2>&1)"; rc=$?
(( rc == 2 )) && ok "I: empty --host exits 2" || fail "I: empty --host exited $rc, want 2"
[[ "$out" == *"--host value must not be empty"* ]] && ok "I: error names the empty --host rule" || fail "I: error does not name the empty --host rule"
[[ "$out" == *"Usage: adopt"* ]] && ok "I: empty --host prints usage" || fail "I: empty --host does not print usage"
diff -q "$TMP_ROOT/i/before" "$TMP_ROOT/i/flake.nix" >/dev/null && ok "I: rejected --host run changed nothing" || fail "I: rejected --host run changed the flake"
out="$(cd "$TMP_ROOT/i" && bash "$SCRIPT" --flake "" --dry-run 2>&1)"; rc=$?
(( rc == 2 )) && ok "I: empty --flake exits 2" || fail "I: empty --flake exited $rc, want 2"
[[ "$out" == *"--flake value must not be empty"* ]] && ok "I: error names the empty --flake rule" || fail "I: error does not name the empty --flake rule"
[[ "$out" == *"Usage: adopt"* ]] && ok "I: empty --flake prints usage" || fail "I: empty --flake does not print usage"
diff -q "$TMP_ROOT/i/before" "$TMP_ROOT/i/flake.nix" >/dev/null && ok "I: rejected --flake run changed nothing" || fail "I: rejected --flake run changed the flake"
(cd "$TMP_ROOT/i" && bash "$SCRIPT" --host VALIDMAC --dry-run >/dev/null) && ok "I: valid --host still works" || fail "I: valid --host run exited non-zero"

# J: one-line entries with a trailing comment (#1269)
mkdir -p "$TMP_ROOT/j"
cat > "$TMP_ROOT/j/flake.nix" <<'EOF'
{
  outputs = { ... }: {
    hosts = {
      MGM9JJ4V3R = {
        # inherits default system "aarch64-darwin"
        extraModules = [ ./gpu.nix ];
      };
      COMMENTED = {}; # laptop
      LASTONE = { system = "x86_64-darwin"; }; # last host
    };
  };
}
EOF
(cd "$TMP_ROOT/j" && bash "$SCRIPT" COMMENTED --host NEWMAC >/dev/null) || fail "J: copying a commented one-line host exited non-zero"
grep -q '^      NEWMAC = {}; # laptop$' "$TMP_ROOT/j/flake.nix" && ok "J: commented one-line entry copied exactly" || fail "J: commented one-line entry copy missing or over-copied"
[[ "$(grep -c 'extraModules' "$TMP_ROOT/j/flake.nix")" == 1 ]] && ok "J: copy did not swallow the preceding multiline host" || fail "J: copy swallowed other hosts"
[[ "$(grep -c 'LASTONE' "$TMP_ROOT/j/flake.nix")" == 1 ]] && ok "J: copy did not swallow the following host" || fail "J: copy swallowed the following host"
(cd "$TMP_ROOT/j" && bash "$SCRIPT" LASTONE --host COPYMAC >/dev/null) || fail "J: copying a commented last host exited non-zero"
grep -q '^      COPYMAC = { system = "x86_64-darwin"; }; # last host$' "$TMP_ROOT/j/flake.nix" && ok "J: commented last-host entry copied exactly" || fail "J: commented last-host entry copy missing or over-copied"
[[ "$(grep -c '^    };$' "$TMP_ROOT/j/flake.nix")" == 1 ]] && ok "J: hosts map still closes exactly once" || fail "J: hosts map closing braces duplicated (invalid Nix)"
[[ "$(grep -c 'x86_64-darwin' "$TMP_ROOT/j/flake.nix")" == 2 ]] && ok "J: last-host body present exactly twice (original + copy)" || fail "J: last-host body count wrong after copy"

# K: a template file that cannot be read fails loudly, changing nothing (#1361)
new_flake "$TMP_ROOT/k"
printf 'extraModules = [ ];\n' > "$TMP_ROOT/k/template.nix"
cp "$TMP_ROOT/k/flake.nix" "$TMP_ROOT/k/flake.before"
mkdir -p "$TMP_ROOT/kbin"
printf '#!/bin/sh\nexit 1\n' > "$TMP_ROOT/kbin/sed"
chmod +x "$TMP_ROOT/kbin/sed"
out="$(cd "$TMP_ROOT/k" && PATH="$TMP_ROOT/kbin:$PATH" bash "$SCRIPT" ./template.nix 2>&1)" \
  && fail "K: unreadable template exited 0" || ok "K: unreadable template fails"
[[ "$out" == *"cannot read template file './template.nix'"* ]] \
  && ok "K: error names the template file" || fail "K: error does not name the template file: $out"
cmp -s "$TMP_ROOT/k/flake.before" "$TMP_ROOT/k/flake.nix" \
  && ok "K: flake.nix unchanged" || fail "K: flake.nix changed"


# L: a symlinked --flake is written through to its target (#1557)
mode_of() { stat -c %a "$1" 2>/dev/null || stat -f %Lp "$1"; }
new_flake "$TMP_ROOT/l"
mv "$TMP_ROOT/l/flake.nix" "$TMP_ROOT/l/real-flake.nix"
chmod 600 "$TMP_ROOT/l/real-flake.nix"
ln -s real-flake.nix "$TMP_ROOT/l/flake.nix"
(cd "$TMP_ROOT/l" && bash "$SCRIPT" --host LINKMAC --flake ./flake.nix >/dev/null) || fail "L: adopt through a symlink exited non-zero"
[[ -L "$TMP_ROOT/l/flake.nix" ]] && ok "L: flake.nix is still a symlink" || fail "L: flake.nix symlink was replaced"
[[ "$(readlink "$TMP_ROOT/l/flake.nix")" == "real-flake.nix" ]] && ok "L: symlink target unchanged" || fail "L: symlink target changed"
grep -q '^      LINKMAC = {$' "$TMP_ROOT/l/real-flake.nix" && ok "L: entry written into the symlink target" || fail "L: entry missing from the symlink target"
[[ "$(mode_of "$TMP_ROOT/l/real-flake.nix")" == "600" ]] && ok "L: target mode 600 preserved" || fail "L: target mode is $(mode_of "$TMP_ROOT/l/real-flake.nix"), want 600"

# M: a non-644 regular flake keeps its mode after an edit (#1557)
new_flake "$TMP_ROOT/m"
chmod 640 "$TMP_ROOT/m/flake.nix"
(cd "$TMP_ROOT/m" && bash "$SCRIPT" --host MODEMAC >/dev/null) || fail "M: adopt exited non-zero"
grep -q '^      MODEMAC = {$' "$TMP_ROOT/m/flake.nix" && ok "M: MODEMAC added" || fail "M: MODEMAC missing"
[[ "$(mode_of "$TMP_ROOT/m/flake.nix")" == "640" ]] && ok "M: mode 640 preserved" || fail "M: mode is $(mode_of "$TMP_ROOT/m/flake.nix"), want 640"

# N: a symlinked --flake with an absolute target is written through (#1561)
new_flake "$TMP_ROOT/n"
mv "$TMP_ROOT/n/flake.nix" "$TMP_ROOT/n/real-flake.nix"
chmod 600 "$TMP_ROOT/n/real-flake.nix"
ln -s "$TMP_ROOT/n/real-flake.nix" "$TMP_ROOT/n/flake.nix"
(cd "$TMP_ROOT/n" && bash "$SCRIPT" --host ABSMAC --flake ./flake.nix >/dev/null) || fail "N: adopt through an absolute symlink exited non-zero"
[[ -L "$TMP_ROOT/n/flake.nix" ]] && ok "N: flake.nix is still a symlink" || fail "N: flake.nix symlink was replaced"
[[ "$(readlink "$TMP_ROOT/n/flake.nix")" == "$TMP_ROOT/n/real-flake.nix" ]] && ok "N: symlink target unchanged" || fail "N: symlink target changed"
grep -q '^      ABSMAC = {$' "$TMP_ROOT/n/real-flake.nix" && ok "N: entry written into the absolute symlink target" || fail "N: entry missing from the absolute symlink target"
[[ "$(mode_of "$TMP_ROOT/n/real-flake.nix")" == "600" ]] && ok "N: target mode 600 preserved" || fail "N: target mode is $(mode_of "$TMP_ROOT/n/real-flake.nix"), want 600"

# O: a two-hop symlink chain resolves to the real flake (#1561)
new_flake "$TMP_ROOT/o"
mv "$TMP_ROOT/o/flake.nix" "$TMP_ROOT/o/real-flake.nix"
chmod 600 "$TMP_ROOT/o/real-flake.nix"
ln -s real-flake.nix "$TMP_ROOT/o/hop.nix"
ln -s "$TMP_ROOT/o/hop.nix" "$TMP_ROOT/o/flake.nix"
(cd "$TMP_ROOT/o" && bash "$SCRIPT" --host CHAINMAC --flake ./flake.nix >/dev/null) || fail "O: adopt through a symlink chain exited non-zero"
[[ -L "$TMP_ROOT/o/flake.nix" && -L "$TMP_ROOT/o/hop.nix" ]] && ok "O: both chain links are still symlinks" || fail "O: a chain link was replaced"
[[ "$(readlink "$TMP_ROOT/o/flake.nix")" == "$TMP_ROOT/o/hop.nix" && "$(readlink "$TMP_ROOT/o/hop.nix")" == "real-flake.nix" ]] && ok "O: chain targets unchanged" || fail "O: a chain target changed"
grep -q '^      CHAINMAC = {$' "$TMP_ROOT/o/real-flake.nix" && ok "O: entry written into the chain's real flake" || fail "O: entry missing from the chain's real flake"
[[ "$(mode_of "$TMP_ROOT/o/real-flake.nix")" == "600" ]] && ok "O: target mode 600 preserved" || fail "O: target mode is $(mode_of "$TMP_ROOT/o/real-flake.nix"), want 600"

if (( failures )); then
  echo "$failures failure(s)" >&2
  exit 1
fi
echo "all adopt tests passed"

