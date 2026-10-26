#!/usr/bin/env bash
# Regression test for opencode_pids() in opencode-db-vacuum.sh and
# opencode-db-prune.sh.
#
# opencode_pids reads `ps -eo pid,ppid,args` and filters it in awk. This
# test shadows ps with synthetic rows and checks which pids come back:
#
#   counted:  a bare "opencode" argv[0] (the normal PATH invocation --
#             #467: clearing $1/$2 in awk rebuilt $0 with OFS padding,
#             so "(^|/)opencode" never anchored at the real argv[0] and
#             this shape was silently missed), "./opencode", and
#             absolute-path forms.
#   excluded: this script itself, its invocation chain (ancestors whose
#             cmdlines name the script), and its descendants (#465);
#             processes whose cmdline merely mentions opencode as a
#             later argument, unrelated processes, and argv[0]s that
#             merely START with opencode (opencode-foo, opencodefoo,
#             opencode-server, opencode.bak -- #828: the awk test needs
#             a boundary after the name, not just a prefix).
#
# Run: ./opencode-db-pids.test.sh [script ...]
# With no arguments it tests both db scripts next to this file.
set -u

cd "$(dirname "$0")" || exit
if [ "$#" -gt 0 ]; then
  scripts=("$@")
else
  scripts=(./opencode-db-vacuum.sh ./opencode-db-prune.sh)
fi

failures=0

# Fixture pids are offset far above any real pid so they cannot collide
# with the test shell's own pid (self) or its real ancestors.
B=$(( $$ + 100000 ))

fixture_for() {
  # $1 = name of the script under test, used in the self/ancestor rows.
  cat <<EOF
$B 1 opencode serve
$((B + 1)) 1 /usr/local/bin/opencode serve
$((B + 2)) 1 ./opencode serve
$((B + 3)) 1 /home/user/.opencode/bin/opencode serve
$((B + 50)) 1 bash $1 --dry-run
$$ $((B + 50)) bash $1 --dry-run
$((B + 51)) $$ opencode serve
$((B + 7)) 1 opencode-foo serve
$((B + 8)) 1 opencodefoo serve
$((B + 9)) 1 /usr/local/bin/opencode-server
$((B + 10)) 1 /usr/local/bin/opencode.bak serve
$((B + 60)) 1 vim notes.txt
$((B + 61)) 1 watch opencode serve
EOF
}

want=$(printf '%s\n' "$B" "$((B + 1))" "$((B + 2))" "$((B + 3))" | sort -n)

for script in "${scripts[@]}"; do
  fixture=$(mktemp)
  fixture_for "$script" > "$fixture"

  # Shadow ps with the fixture, then load opencode_pids from the script
  # under test. $$ inside the function still names this test shell (the
  # main script pid), matching the fixture's self row.
  ps() { cat "$fixture"; }
  eval "$(awk '/^opencode_pids\(\) \{/,/^\}/' "$script")"
  got=$(opencode_pids | sort -n)
  unset -f ps
  rm -f "$fixture"

  if [ "$got" = "$want" ]; then
    printf 'PASS %s: bare, ./, and path opencode counted; self/ancestor/descendant, non-argv0 mentions and opencode-prefixed argv0s excluded\n' "$script"
  else
    printf 'FAIL %s\n  want pids:\n%s\n  got pids:\n%s\n' "$script" "$want" "$got" >&2
    failures=$((failures + 1))
  fi
done

[ "$failures" -eq 0 ]
