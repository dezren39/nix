#!/usr/bin/env bash
# Regression test for install-vscode-patch-agent.sh (#945).
#
# #945: the installer printed "Installed and loaded" and exited 0 even
# when nothing was installed -- ~/Library/LaunchAgents was never created
# (so the plist write failed on a fresh account), and a failed
# `launchctl load` was never checked.
#
# This test drives the real installer with a scratch HOME and a stub
# launchctl on PATH:
#   missing-dir:  no LaunchAgents dir -> created, plist written, exit 0
#   failing load: stub load exits 42 -> exit non-zero naming the load
#                 step, no success banner (the plist write did happen)
#   blocked dir:  LaunchAgents is a regular file -> exit non-zero
#                 naming the directory/write step, no success banner
#   happy path:   dir pre-exists -> exit 0, banner, plist byte-identical
#                 to the missing-dir run
#
# Run: bash install-vscode-patch-agent.test.sh (exit 0 = all passed)
set -u

cd "$(dirname "$0")" || exit 1
INSTALLER="./install-vscode-patch-agent.sh"
PLIST_REL="Library/LaunchAgents/com.drewry.patch-vscode-insiders.plist"
failures=0

check() { # $1 name, $2 condition-result (0 = pass)
  if [ "$2" -eq 0 ]; then
    echo "PASS: $1"
  else
    echo "FAIL: $1"
    failures=$((failures + 1))
  fi
}

make_home() { # $1 = home dir; seeds the patch script the installer requires
  mkdir -p "$1/nix"
  printf '# stub patch script\n' > "$1/nix/patch-vscode-insiders.sh"
}

make_stub() { # $1 = stub bin dir
  mkdir -p "$1"
  printf '#!/bin/sh\nif [ "$1" = "load" ]; then exit "${STUB_LOAD_RC:-0}"; fi\nexit 0\n' > "$1/launchctl"
  chmod +x "$1/launchctl"
}

run_installer() { # $1 = home, $2 = stub bin, $3 = load rc, $4 = output file; prints exit code
  HOME="$1" PATH="$2:$PATH" STUB_LOAD_RC="$3" bash "$INSTALLER" > "$4" 2>&1
  echo $?
}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
stub="$work/bin"
make_stub "$stub"

# missing-dir: LaunchAgents does not exist; the installer must create it.
home1="$work/home1"
make_home "$home1"
rc=$(run_installer "$home1" "$stub" 0 "$work/out1")
[ "$rc" -eq 0 ]; check "945 missing-dir exits 0" $?
[ -f "$home1/$PLIST_REL" ]; check "945 missing-dir plist written" $?
grep -q "Installed and loaded" "$work/out1"; check "945 missing-dir banner printed" $?
grep -q "<string>com.drewry.patch-vscode-insiders</string>" "$home1/$PLIST_REL" 2>/dev/null
check "945 missing-dir plist names the agent" $?
cp "$home1/$PLIST_REL" "$work/golden.plist" 2>/dev/null

# failing load: stub launchctl load exits 42.
home2="$work/home2"
make_home "$home2"
mkdir -p "$home2/Library/LaunchAgents"
rc=$(run_installer "$home2" "$stub" 42 "$work/out2")
[ "$rc" -ne 0 ]; check "945 failing load exits non-zero" $?
grep -q "launchctl load failed" "$work/out2"; check "945 failing load names the load step" $?
grep -q "Installed and loaded" "$work/out2"; [ $? -ne 0 ]; check "945 failing load prints no success banner" $?
[ -f "$home2/$PLIST_REL" ]; check "945 failing load still wrote the plist" $?

# blocked dir: LaunchAgents exists as a regular file, so it cannot be
# created or written into.
home3="$work/home3"
make_home "$home3"
mkdir -p "$home3/Library"
printf 'not a directory\n' > "$home3/Library/LaunchAgents"
rc=$(run_installer "$home3" "$stub" 0 "$work/out3")
[ "$rc" -ne 0 ]; check "945 blocked dir exits non-zero" $?
grep -q "ERROR" "$work/out3"; check "945 blocked dir reports an error" $?
grep -q "Installed and loaded" "$work/out3"; [ $? -ne 0 ]; check "945 blocked dir prints no success banner" $?

# happy path: directory pre-exists; plist must be byte-identical to the
# missing-dir run, banner printed, exit 0.
home4="$work/home4"
make_home "$home4"
mkdir -p "$home4/Library/LaunchAgents"
rc=$(run_installer "$home4" "$stub" 0 "$work/out4")
[ "$rc" -eq 0 ]; check "945 happy path exits 0" $?
grep -q "Installed and loaded" "$work/out4"; check "945 happy path banner printed" $?
cmp -s "$home4/$PLIST_REL" "$work/golden.plist"; check "945 happy path plist byte-identical" $?

if [ "$failures" -gt 0 ]; then
  echo "FAILURES: $failures"
  exit 1
fi
echo "ALL PASS"
