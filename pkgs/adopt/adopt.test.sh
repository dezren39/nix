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

if (( failures )); then
  echo "$failures failure(s)" >&2
  exit 1
fi
echo "all adopt tests passed"
