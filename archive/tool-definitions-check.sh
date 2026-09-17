#!/usr/bin/env bash
# Compares vendored tool-definitions/*.txt against the running opencode binary.
set -uo pipefail
BIN="$(readlink -f "$(command -v opencode)")"
BIN="$(dirname "$BIN")/.opencode-wrapped"
[ -f "$BIN" ] || BIN="$(readlink -f "$(command -v opencode)")"
echo "running: $(opencode --version 2>/dev/null)"
S=$(mktemp); strings -a "$BIN" > "$S" 2>/dev/null
rc=0
for f in tool-definitions/*.txt; do
  t=$(basename "$f" .txt); miss=0
  while IFS= read -r line; do
    [ -z "$line" ] && continue
    grep -qF -- "$line" "$S" || miss=$((miss+1))
  done < "$f"
  if [ "$miss" -eq 0 ]; then printf "  ok      %s\n" "$t"
  else printf "  DRIFT   %s (%d lines not in binary)\n" "$t" "$miss"; rc=1; fi
done
rm -f "$S"; exit $rc
