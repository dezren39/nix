#!/usr/bin/env bash
# Regression test for opencode-tmp-guard (#453).
#
# #453: the guard embedded in opencode-tmp-guard.nix scanned for repos
# with `find "$T" -maxdepth 4`, so a worktree's .git pointer file in a
# nested layout and any repo at depth >= 5 never had its .git metadata
# refreshed -- exactly the files macOS dirhelper sweeps first (#377).
# And a regular file at $TMPDIR/opencode matched none of the
# link-maintenance branches, so every run was a silent no-op.
#
# This test extracts the REAL guard script from opencode-tmp-guard.nix
# (unescaping the nix ''${...} sequences), strips the absolute macOS
# tool paths so tools resolve from PATH, stubs `getconf` to point
# DARWIN_USER_TEMP_DIR at a fixture tree, and drives the real script:
#   direct repo:   $T/repo/.git/HEAD                       -> refreshed
#   worktree ptr:  $T/work/repo/.worktrees/wt1/.git (file) -> refreshed
#   deep repo:     $T/a/b/c/deeprepo/.git/HEAD             -> refreshed
#   node_modules:  $T/nmhost/node_modules/dep/.git/HEAD    -> NOT refreshed (pruned)
#   regular file:  $T/opencode is a regular file           -> WARN logged, file kept
#   missing link:  $T/opencode absent                      -> symlink created
#
# Run: bash opencode-tmp-guard.test.sh (exit 0 = all passed)
set -u

cd "$(dirname "$0")" || exit 1
failures=0

check() { # $1 name, $2 condition-result (0 = pass)
  if [ "$2" -eq 0 ]; then
    echo "PASS: $1"
  else
    echo "FAIL: $1"
    failures=$((failures + 1))
  fi
}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# Extract the guard script body from the nix module.
python3 - "$work/guard.sh" <<'PYEOF'
import re, sys
src = open("opencode-tmp-guard.nix").read()
m = re.search(r'writeShellScript "opencode-tmp-guard" +\'\'\n(.*?)\n  \'\'', src, re.S)
if not m:
    sys.exit("could not extract guard script from opencode-tmp-guard.nix")
body = m.group(1).replace("''${", "${")
# Nix indented strings strip the common leading indentation from every
# line; the built script is de-indented (its heredoc delimiters sit at
# column 0), so extraction must do the same.
lines = body.split("\n")
indent = min(len(l) - len(l.lstrip(" ")) for l in lines if l.strip())
body = "\n".join(l[indent:] if l.startswith(" " * indent) else l for l in lines)
body = body.replace("/usr/bin/", "").replace("/bin/", "")
open(sys.argv[1], "w").write(body + "\n")
PYEOF
[ -s "$work/guard.sh" ]; check "453 guard script extracted from the nix module" $?
grep -q "maxdepth" "$work/guard.sh"; [ $? -ne 0 ]; check "453 repo scan has no depth cap" $?

# Stub getconf: DARWIN_USER_TEMP_DIR -> $GUARD_TEST_T.
mkdir -p "$work/bin"
cat > "$work/bin/getconf" <<'EOF'
#!/bin/sh
if [ "$1" = "DARWIN_USER_TEMP_DIR" ]; then printf '%s\n' "$GUARD_TEST_T"; exit 0; fi
exit 1
EOF
chmod +x "$work/bin/getconf"

OLD_T=202001010000 # atime far past dirhelper's 3-day window

seed_gitdir() { # $1 = .git directory; seeds a HEAD with an old atime
  mkdir -p "$1"
  printf 'ref: refs/heads/main\n' > "$1/HEAD"
  touch -a -t $OLD_T "$1/HEAD"
}

refreshed() { # $1 = file; 0 if its atime is newer than the reference marker
  [ -n "$(find "$1" -anewer "$work/ref" 2>/dev/null)" ]
}

run_guard() { # $1 = T dir, $2 = HOME dir, $3 = output file; prints exit code
  GUARD_TEST_T="$1" HOME="$2" PATH="$work/bin:$PATH" bash "$work/guard.sh" > "$3" 2>&1
  echo $?
}

# --- Scenario 1: repo scan depths + missing link -------------------
T1="$work/T1"
HOME1="$work/home1"
mkdir -p "$T1" "$HOME1"
seed_gitdir "$T1/repo/.git"
mkdir -p "$T1/work/repo/.worktrees/wt1"
printf 'gitdir: %s\n' "$T1/work/repo/.git/worktrees/wt1" > "$T1/work/repo/.worktrees/wt1/.git"
touch -a -t $OLD_T "$T1/work/repo/.worktrees/wt1/.git"
seed_gitdir "$T1/a/b/c/deeprepo/.git"
seed_gitdir "$T1/nmhost/node_modules/dep/.git"
touch "$work/ref"

rc=$(run_guard "$T1" "$HOME1" "$work/out1")
[ "$rc" -eq 0 ]; check "453 scan run exits 0" $?
[ -L "$T1/opencode" ]; check "453 missing link: symlink created" $?
[ "$(readlink "$T1/opencode")" = "$HOME1/.local/share/opencode/tmp/opencode" ]
check "453 missing link: symlink points at the durable directory" $?
refreshed "$T1/repo/.git/HEAD"; check "453 direct repo .git refreshed" $?
refreshed "$T1/work/repo/.worktrees/wt1/.git"; check "453 worktree pointer file refreshed" $?
refreshed "$T1/a/b/c/deeprepo/.git/HEAD"; check "453 deep repo (depth 5) .git refreshed" $?
refreshed "$T1/nmhost/node_modules/dep/.git/HEAD"; [ $? -ne 0 ]
check "453 node_modules .git NOT refreshed (pruned)" $?
grep -q "refreshed .git metadata in 3 repo(s)" "$work/out1"
check "453 scan run reports exactly the 3 real repos" $?

# --- Scenario 2: regular file at $TMPDIR/opencode ------------------
T2="$work/T2"
HOME2="$work/home2"
mkdir -p "$T2" "$HOME2"
printf 'user data, not a directory\n' > "$T2/opencode"

rc=$(run_guard "$T2" "$HOME2" "$work/out2")
[ "$rc" -eq 0 ]; check "453 regular-file run exits 0" $?
grep -q "WARN" "$work/out2"; check "453 regular file: WARN logged" $?
grep -q "$T2/opencode" "$work/out2"; check "453 regular file: WARN names the path" $?
[ ! -L "$T2/opencode" ] && [ "$(cat "$T2/opencode")" = "user data, not a directory" ]
check "453 regular file: left in place, content untouched" $?

if [ "$failures" -gt 0 ]; then
  echo "FAILURES: $failures"
  exit 1
fi
echo "ALL PASS"
