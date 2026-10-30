#!/usr/bin/env bash
# adopt: add this Mac as a darwin host in flake.nix
#
# A host in this flake is one entry in the `hosts` map in flake.nix: the key
# is the machine's LocalHostName (what darwin-rebuild matches on), and the
# value says how it differs from defaultDarwinSystem -- usually nothing, so
# adopting a new Mac is one small block. This script writes that block, from
# a template, so a new (or freshly reinstalled, renamed) Mac is:
#
#   nix run .#adopt                     # this Mac, default template
#   nix run .#adopt -- MGM9JJ4V3R       # copy an existing host's entry
#   nix run .#adopt -- ./host-body.nix  # body from a template file
#   ./s                                 # build + switch onto it
#
# Idempotent: adopting a host that already has an entry changes nothing.
set -euo pipefail

usage() {
  cat <<EOF
Usage: adopt [TEMPLATE] [--host NAME] [--flake FILE] [--dry-run]

Add the current Mac to the hosts map in flake.nix, creating its
darwinConfiguration from a template.

TEMPLATE is one of:
  default          an entry that inherits everything (the default)
  <existing host>  copy that host's entry from flake.nix (e.g. MGM9JJ4V3R)
  <file>           a file whose contents become the entry's body

Options:
  --host NAME   host name to create (default: this Mac's LocalHostName,
                via scutil, falling back to hostname -s)
  --flake FILE  flake.nix to edit (default: ./flake.nix)
  --dry-run     print the entry that would be added, change nothing
  -h, --help    this help
EOF
}

template="default"
template_set=0
host=""
host_set=0
flake="./flake.nix"
flake_set=0
dry_run=0
while (( $# )); do
  case "$1" in
    --host)    [[ $# -ge 2 && "$2" != --* ]] || { echo "adopt: --host needs a value" >&2; exit 2; }
               [[ -n "$2" ]] || { echo "adopt: --host value must not be empty" >&2; usage >&2; exit 2; }
               if (( host_set )); then
                 echo "adopt: --host may be given only once" >&2
                 usage >&2
                 exit 2
               fi
               host="$2"; host_set=1; shift ;;
    --flake)   [[ $# -ge 2 && "$2" != --* ]] || { echo "adopt: --flake needs a value" >&2; exit 2; }
               [[ -n "$2" ]] || { echo "adopt: --flake value must not be empty" >&2; usage >&2; exit 2; }
               if (( flake_set )); then
                 echo "adopt: --flake may be given only once" >&2
                 usage >&2
                 exit 2
               fi
               flake="$2"; flake_set=1; shift ;;
    --dry-run) dry_run=1 ;;
    -h|--help) usage; exit 0 ;;
    -*)        echo "adopt: unknown option $1" >&2; usage >&2; exit 2 ;;
    *)         if (( template_set )); then
                 echo "adopt: unexpected extra argument '$1' (only one TEMPLATE may be given)" >&2
                 usage >&2
                 exit 2
               fi
               template="$1"; template_set=1 ;;
  esac
  shift
done

[[ -f "$flake" ]] || { echo "adopt: no flake at $flake (run from the repo root, or pass --flake)" >&2; exit 1; }

# This Mac's name, the way darwin-rebuild matches it (flake.nix: darwin-
# rebuild matches via scutil --get LocalHostName).
if [[ -z "$host" ]]; then
  if command -v scutil >/dev/null 2>&1; then
    host="$(scutil --get LocalHostName)"
  else
    host="$(hostname -s)"
  fi
fi
[[ "$host" =~ ^[A-Za-z0-9][A-Za-z0-9_-]*$ ]] ||
  { echo "adopt: host name '$host' is not a valid flake attribute name" >&2; exit 2; }

# Print the indented entry block for host $2 from template $1 to stdout.
make_block() {
  local tmpl="$1" name="$2"
  if [[ "$tmpl" == default ]]; then
    cat <<EOF
      $name = {
        # inherits default system "aarch64-darwin"
        # extraModules = [ ];
        # extraSpecialArgs = { };
      };
EOF
  elif [[ -f "$tmpl" ]]; then
    echo "      $name = {"
    sed 's/^/        /' "$tmpl"
    echo "      };"
  else
    [[ "$tmpl" =~ ^[A-Za-z0-9][A-Za-z0-9_-]*$ ]] ||
      { echo "adopt: template '$tmpl' is not a host name or a file" >&2; return 2; }
    local block
    block="$(awk -v n="$tmpl" '
      $0 ~ "^      " n " = [{]" { found = 1; start = NR }
      found { print }
      found && NR == start && /};[[:space:]]*$/ { exit }
      found && /^      };$/ { exit }
    ' "$flake")"
    [[ -n "$block" ]] ||
      { echo "adopt: no host '$tmpl' in $flake to copy (existing: $(list_hosts))" >&2; return 1; }
    sed "1s/^      [^ ]* = {/      $name = {/" <<<"$block"
  fi
}

# Host names currently in the hosts map, space-separated (for messages).
list_hosts() {
  awk '
    /^    hosts = [{]$/ { in_hosts = 1; next }
    in_hosts && /^    };$/ { exit }
    in_hosts && /^      [A-Za-z0-9_-]+ = [{]/ { printf "%s ", $1 }
  ' "$flake"
}

for existing in $(list_hosts); do
  if [[ "$existing" == "$host" ]]; then
    echo "adopt: $host is already a host in $flake; nothing to do"
    exit 0
  fi
done

block="$(make_block "$template" "$host")"

if (( dry_run )); then
  echo "adopt: would add to $flake (template: $template):"
  echo "$block"
  exit 0
fi

tmp="$(mktemp "${flake}.adopt.XXXXXX")"
blockfile="$(mktemp "${flake}.adopt-block.XXXXXX")"
trap 'rm -f "$tmp" "$blockfile"' EXIT
printf '%s\n' "$block" >"$blockfile"
# The block reaches awk through a file, never a -v assignment: macOS awk
# (BWK) rejects a literal newline in a -v value ("newline in string"), so
# the first version of this script died here on a Mac -- and its || handler
# then misreported that as a missing hosts map (#1074). GNU awk and mawk
# accept multiline -v, which is why Linux testing missed it.
awk -v blockfile="$blockfile" '
  /^    hosts = [{]$/ {
    print
    while ((getline line < blockfile) > 0) print line
    close(blockfile)
    done = 1
    next
  }
  { print }
  END { if (!done) exit 1 }
' "$flake" >"$tmp" || { echo "adopt: no 'hosts = {' map found in $flake" >&2; exit 1; }
chmod 644 "$tmp"
mv "$tmp" "$flake"
trap - EXIT
rm -f "$blockfile"

# No nixfmt pass: the inserted block is written in nixfmt style already, and
# flake.nix carries pre-existing formatting drift, so formatting the whole
# file here would bury the one-entry change under unrelated reformatting.

echo "adopt: added host $host to $flake (template: $template)"
echo "adopt: next: nix eval .#darwinConfigurations.$host.system.drvPath  # sanity, then ./s"
