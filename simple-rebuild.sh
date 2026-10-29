#!/usr/bin/env zsh
path=("$HOME/.nix-profile/bin" "/nix/var/nix/profiles/default/bin" "/run/current-system/sw/bin" "$path[@]")

# Trace every command with the file and line it came from.
#
# zsh's default PS4 is '+%N:%i> ', where %N is the *script or function* name
# and %i the line within it. At top level that reads as simple-rebuild.sh:97,
# which is what you want; inside a function it collapses to phase_build:2,
# which cannot be looked up in the file. That became the common case when this
# script was split into phases.
#
# %x is the file containing the source, %I the line number in that file, so
# both cases report a real location you can open.
PS4='+%x:%I> '
set -exuo pipefail

# error: opening Git repository "/Users/drewry.pope/.config/nix": repository path '/Users/drewry.pope/.config/nix' is not owned by current user
# # if not root rerun as root
# if [[ $EUID -ne 0 ]]; then
#     echo "Rerunning as root..."
#     sudo "$0"
#     exit $?
# else
#     echo "Running as root..."
# fi
#ulimit -n $(ulimit -Hn)
#sudo prlimit --pid $$ --nofile=1000000:1000000
#nix-shell -p nixVersions.nix_2_18 git cachix jq
#cat /mnt/c/wsl/cachix.key | cachix authtoken --stdin
# Get the directory of the script.
#
# This was `$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)`, which is bash. The
# shebang is zsh, where BASH_SOURCE does not exist, so under `set -u` the line
# errored with "BASH_SOURCE[0]: parameter not set", script_dir came out empty,
# and the `cd` below became `cd ''` -- a no-op that silently left the script in
# whatever directory the caller happened to be in. It only ever worked because
# callers were already here. `${0:A:h}` is the zsh spelling: :A absolutises and
# resolves symlinks (so ~/nix resolves to ~/.config/nix), :h takes the dirname.
script_dir="${0:A:h}"
# Git add for the script's directory
cd "${script_dir}" || exit 1
echo "entered: $script_dir"
echo "git add ."

# Reclaim any root-owned files, but only if there are any.
#
# This was an unconditional `sudo chown -R`, which prompted for a password
# before anything else happened even when there was nothing to fix. It dates
# from when `sudo nix run nix-darwin -- switch` did the build *and* the
# activation as root, leaving every eval/build artifact in this directory
# root-owned (see the comment above the build below). Building unprivileged
# fixed the cause; measured on 2026-10-01 after two unprivileged builds, the
# repo had 0 files not owned by the invoking user.
#
# Kept as a guard rather than deleted because the activation below still runs
# nix as root against this flake, so it is not proven that nothing can slip
# through. The difference is that it now costs a password only when it finds
# something, and says what it found.
reclaim_root_owned() {
  local owner="${USER:-$(id -un)}" strays
  strays=$(find . -not -user "$owner" -not -path './.git/*' -print -quit 2>/dev/null)
  if [[ -n "$strays" ]]; then
    echo "reclaiming root-owned files (first: $strays)"
    sudo chown -R "$owner" .
  else
    echo "no root-owned files; skipping chown"
  fi
}

reclaim_root_owned

# Is the git on PATH actually usable? On a clean macOS install the only git
# is /usr/bin/git, an Xcode Command Line Tools shim: invoking it pops the CLT
# install dialog and exits non-zero, which under `set -e` killed ./s at the
# `git add .` below -- before anything was built, let alone switched (#1069).
# Git in this script is bookkeeping only (staging so the flake sees new
# files, and the marker commits), so when it is missing or is the
# uninstalled CLT shim, skip the git steps for this run instead of gating
# the switch on Xcode. The probe must not invoke git itself (that is what
# triggers the shim's dialog): /usr/bin/git counts as usable only when
# xcode-select reports a developer directory (CLT or Xcode.app) behind it;
# a git from the nix profile or the current system is usable as-is. Marker
# commits simply resume on the next run from an installed system.
git_ok=1
if ! command -v git >/dev/null 2>&1; then
  git_ok=0
elif [[ "$(command -v git)" == /usr/bin/git ]] && ! /usr/bin/xcode-select -p >/dev/null 2>&1; then
  git_ok=0
fi
if (( git_ok )); then
  git add .
else
  echo "no usable git (missing, or the Xcode CLT shim on a clean install); skipping git add + marker commits for this run"
fi

# Keep Nix's GitHub token current. nix.conf (this directory, so tracked)
# `include`s ~/.local/share/nix/access-tokens.conf, which holds the token and
# must never be inside the repo: `git add .` above would commit it. Rewritten
# from `gh auth token` on every run, so a rotated gh token cannot leave Nix
# sending a stale one (a bad token fails every github: fetch with 401, which is
# worse than the anonymous 60/hour it replaces). If gh is missing or logged
# out, the old file is left alone.
refresh_nix_access_token() {
  local dir="$HOME/.local/share/nix" tok
  tok=$(command -v gh >/dev/null && gh auth token 2>/dev/null) || tok=""
  if [[ -z "$tok" ]]; then
    echo "gh token unavailable; leaving Nix access token as is" >&2
    return 0
  fi
  mkdir -p "$dir" && chmod 700 "$dir"
  ( umask 077
    print -r -- "access-tokens = github.com=$tok" >"$dir/access-tokens.conf.tmp" &&
      mv "$dir/access-tokens.conf.tmp" "$dir/access-tokens.conf" )
  echo "refreshed Nix GitHub access token"
}
# Not traced: the token must not reach the `set -x` log.
{ set +x; } 2>/dev/null
refresh_nix_access_token
set -x

# Phases.
#
# `just build`, `just activate` and `just switch` are three entry points into
# one implementation, so there is nothing to drift. The split is by privilege:
# phase_build needs no sudo and can run unattended, phase_activate needs root
# and therefore a human (or a fingerprint) at the machine.
#
#   --build-only     prepare + build            no sudo
#   --activate-only  activate + post + commit   sudo
#   (no flag)        all of it
#   --no-commit      combinable with any of the above: still stages (flakes
#                    only see tracked files) but skips the marker commits, for
#                    iterating on a build that is not ready to be recorded
mode="all"
generation=""
no_commit=0
usage="usage: ${0:t} [--no-commit] [--build-only|--activate-only [GENERATION]]"
while (( $# )); do
  case "$1" in
    --build-only)    mode="build" ;;
    --activate-only)
      mode="activate"
      if [[ -n "${2:-}" && "$2" != --* ]]; then generation="$2"; shift; fi
      ;;
    --no-commit)     no_commit=1 ;;
    *) echo "$usage" >&2; exit 2 ;;
  esac
  shift
done

# Validate GENERATION whole, here at parse time (#1022), in the shape of
# the HEALTH_TIMEOUT block below (#880) and branch-cleanup's inactive_days
# (#1015). The parse above takes any token not starting with `--` as the
# generation, and the value's only use is the rollback path's
# `sudo darwin-rebuild --switch-generation "$generation"` -- so -1, abc,
# 3.5, 0, a 26-digit string, and even the single-dash typo -no-commit (with
# no_commit left unset) were all accepted here and failed only later,
# after a sudo prompt, inside darwin-rebuild's own parser. Judge it now,
# before any phase: digits only, positive, with an INT_MAX range backstop
# judged on the stripped value by digit count first, then by value (at
# most 10 digits always fits zsh's 64-bit arithmetic). Leading zeros are
# normalized away, as they would otherwise be read as octal downstream.
if [[ -n "$generation" ]]; then
  case "$generation" in
    *[!0-9]*|'')
      echo "simple-rebuild.sh: --activate-only GENERATION must be a positive integer (got: $generation)" >&2
      exit 2 ;;
  esac
  _gen="$generation"
  while [[ "$_gen" == 0?* ]]; do _gen="${_gen#0}"; done
  if (( ${#_gen} > 10 || _gen > 2147483647 || _gen <= 0 )); then
    echo "simple-rebuild.sh: --activate-only GENERATION must be a positive integer (got: $generation)" >&2
    exit 2
  fi
  generation="$_gen"
  unset _gen
fi

# Every marker commit goes through here so --no-commit has one place to act.
commit_marker() {
  if (( no_commit )); then
    echo "--no-commit: skipping commit \"$1\""
    return 0
  fi
  if (( ! git_ok )); then
    echo "no usable git: skipping commit \"$1\""
    return 0
  fi
  git commit --no-verify --allow-empty -m "$1"
}

# Every commit here is --allow-empty: the point is to mark *when* a build or an
# activation happened, which is information even when no file changed. A build
# names the store path it produced, because that is the only identity a build
# has -- and reading it needs no sudo, unlike --list-generations, which takes
# the system profile lock. An activation names the generation, which is the
# identity the system has.
phase_build() {
  echo "nix build .#darwinConfigurations.$(hostname -s).system  (as $USER)"
  # `nix build` can exit 0 without updating ./result: a non-fatal internal
  # error (observed: "error (ignored): SQLite database ... is busy", eval-
  # cache lock contention from a concurrent nix process) made it skip the
  # out-link entirely, exit code and all. `set -e` cannot catch that --
  # nothing failed from the shell's point of view -- so basename of a stale
  # or absent ./result ("result", unresolved) was committed as the built
  # store path, and the real failure surfaced three steps later as phase_
  # activate's unrelated-looking "no ./result to activate" (2026-10-19).
  # Capture the real exit status first (set -e would otherwise end the
  # script on a nonzero one before the stale-./result message below can
  # tell it apart from this case), then require the same executable
  # phase_activate checks for, before anything is read from ./result.
  local build_status=0
  nix --extra-experimental-features 'nix-command flakes' \
    build ".#darwinConfigurations.$(hostname -s).system" --keep-going --out-link ./result \
    || build_status=$?
  if (( build_status == 0 )) && [[ -x ./result/sw/bin/darwin-rebuild ]]; then
    built=$(basename "$(readlink -f ./result)")
  else
    # A failed build stops here, in every mode, and nothing is activated or
    # committed.
    #
    # This used to fall back to `sudo nix run nix-darwin -- switch` outside
    # --build-only. Nothing the unprivileged build fails on is fixed by
    # retrying it as root -- a broken patch or hash fails the same way -- so
    # the only failures it ever "fixed" were flaky ones, by rebuilding the
    # failed derivation and hoping. It also hid them: the run exited 0 and
    # committed "build root-fallback". And it activated twice -- the fallback
    # switch, then phase_activate's own -- so each such run made two
    # generations (292/293 and 294/295 on 2026-10-02) and ran every activation
    # step, Homebrew included, twice. Both of those failures were real mcpx
    # test races; see the mcpx commits that day.
    {
      print -P "%F{red}%B==> build failed; nothing activated, nothing committed%b%f"
      if (( build_status != 0 )); then
        echo "    The failing derivation and its last log lines are above."
        echo "    Full log: nix log <the .drv named in 'Cannot build'>"
      else
        echo "    nix build exited 0 but ./result/sw/bin/darwin-rebuild is missing --"
        echo "    it did not actually produce a system. Check the output above for"
        echo "    a non-fatal error nix logged and ignored (eval-cache lock"
        echo "    contention is one seen cause); rerunning usually clears it."
      fi
    } >&2
    return 1
  fi
  commit_marker "$(hostname) build ${built}"
}

# Upgrade Homebrew formulae and casks, as you, on this terminal.
#
# configuration.nix sets homebrew.onActivation.upgrade = false because inside
# activation brew runs on a pty of its own, where your sudo approval does not
# reach, so every cask needing root asked for Touch ID again (measured; see the
# comment there). Here brew's `sudo` calls run on this terminal and reuse one
# approval. `sudo -v` refreshes it, and asks only if the approval from
# darwin-rebuild above has expired (timestamp_timeout=15).
#
# Uses the same Brewfile activation just installed from, so the set of casks
# and their per-cask options (greedy, args) are identical. Homebrew itself was
# already updated during activation (onActivation.autoUpdate), so it is not
# updated again. A failed upgrade is reported but does not undo the switch.
brew_upgrade() {
  local brewfile
  brewfile=$(grep -o '/nix/store/[a-z0-9]*-Brewfile' ./result/activate | head -1)
  if [[ -z "$brewfile" || ! -x /opt/homebrew/bin/brew ]]; then
    echo "skipping brew upgrade: no Brewfile in ./result/activate or no brew" >&2
    return 0
  fi
  echo "brew bundle --upgrade $brewfile (as $USER)"
  sudo -v
  HOMEBREW_NO_AUTO_UPDATE=1 /opt/homebrew/bin/brew bundle install \
    --file="$brewfile" --upgrade --verbose --force ||
    echo "brew upgrade failed; the switch itself succeeded" >&2
}

# Install or update Xcode from the Mac App Store, as you, after the switch.
#
# Xcode is managed, but deliberately NOT through masApps.nix (#1069): brew
# bundle runs *during* activation, so its App Store requirement and
# multi-GB download sat on the switch's critical path and could fail or
# stall it, above all on a clean install. Here -- post-switch, like
# brew_upgrade above -- ./s calls `mas` itself (mas is a system package,
# systemPackages.nix, so it is on PATH from the system profile once a
# switch has installed it):
#
#   Xcode.app absent   mas install 497799835
#   Xcode.app present  mas upgrade 497799835  (no-op when already current)
#
# so every ./s keeps Xcode installed and up to date. It assumes the App
# Store is signed in (Drew, 2026-10-28) -- mas cannot sign in itself on
# modern macOS -- and a failure (signed out, no prior "purchase", network)
# is reported without failing or undoing the switch. Rollbacks return
# before this point and skip it.
#
# Afterwards, point the developer directory at Xcode and accept its
# license / run its first launch here, mirroring configuration.nix's
# postActivation: that copy only runs at the *next* switch, which would
# leave a just-installed Xcode unselected until then.
#
# No separate Command Line Tools install is needed alongside this: full
# Xcode carries the same tools (xcodebuild, the SDKs, and git/clang via
# the xcode-select shims), and once selected it satisfies the
# Xcode-or-CLT requirement of Homebrew and of builds that call xcrun.
xcode_mas() {
  local xcode_app_id=497799835
  if ! command -v mas >/dev/null 2>&1; then
    echo "skipping Xcode: mas not on PATH (a system package; present after the first switch that installs it)" >&2
    return 0
  fi
  if [[ -d /Applications/Xcode.app ]]; then
    echo "mas upgrade $xcode_app_id (Xcode, as $USER)"
    mas upgrade "$xcode_app_id" ||
      echo "Xcode upgrade failed; the switch itself succeeded" >&2
  else
    echo "mas install $xcode_app_id (Xcode, as $USER; this is a multi-GB download)"
    mas install "$xcode_app_id" ||
      echo "Xcode install failed (App Store signed in? Xcode previously \"purchased\"?); the switch itself succeeded" >&2
  fi
  if [[ -d /Applications/Xcode.app/Contents/Developer ]]; then
    echo "xcode-select -s Xcode.app, xcodebuild -license accept + -runFirstLaunch (as root)"
    sudo /usr/bin/xcode-select -s /Applications/Xcode.app/Contents/Developer 2>/dev/null || true
    sudo /usr/bin/xcodebuild -license accept 2>/dev/null || true
    sudo /usr/bin/xcodebuild -runFirstLaunch 2>/dev/null || true
  fi
}

# After a switch, the servers this flake runs as launchd agents must actually
# be up. Without this a broken agent is invisible here: launchd retries it
# quietly and the failure shows up later as an app that cannot connect. On
# 2026-10-19 there were two at once: OpenChamber's start link named a file
# that did not exist (exit 78 on every spawn, nothing in its log), and the
# mcpx launchd daemon had lost its socket to a daemon the CLI started itself,
# so it exited 1 every ~10 s for hours.
#
# Up means the agent has the same pid twice, 3 s apart (a crash loop caught
# mid-run does not count), and its check passes: a URL answers with any HTTP
# status (401 included: OpenChamber requires its UI password), or a `cmd:`
# command exits 0. mcpx's runs a CLI command from $HOME, which succeeds only
# if the daemon is on the socket the CLI computes -- with autostart off it
# cannot start one of its own -- so a daemon running on a stale socket
# fails it (the drift behind the 2026-10-19 crash loop). All agents
# share HEALTH_TIMEOUT seconds (default 60), since activation restarts them
# asynchronously; OpenChamber answered about 10 s after a cold start when
# measured.
#
# Add new long-running agents here. One-shot agents (KeepAlive = false, such
# as colima and naturalScrollingToggle) exit by design and do not belong.
health_services=(
  "org.nixos.openchamber http://127.0.0.1:3000/"
  "org.nixos.mcpx cmd:cd ~ && mcpx ls"
)

# HEALTH_TIMEOUT is used only in zsh arithmetic ($(( SECONDS + ... ))) in
# check_services below, where a bare word silently evaluates to 0 -- so
# HEALTH_TIMEOUT=abc used to collapse the deadline to *now* and report
# still-starting agents as not up after a successful switch, 10junk aborted
# the run with a raw "bad math expression" (under set -e, after the switch
# was already recorded), and a >64-bit value truncated and wrapped negative
# (#880). Judge it whole, once, here before the health check, like clean.sh
# judges its tunables (#829/#862): digits only, positive, with an INT_MAX
# range backstop judged on the stripped value by digit count first, then by
# value (at most 10 digits always fits zsh's 64-bit arithmetic). Unset or
# empty is fine: the ${HEALTH_TIMEOUT:-60} default at the use site supplies
# a known-good literal.
if [[ -n "${HEALTH_TIMEOUT:-}" ]]; then
  case "$HEALTH_TIMEOUT" in
    *[!0-9]*|'')
      echo "simple-rebuild.sh: HEALTH_TIMEOUT must be a positive integer (got: $HEALTH_TIMEOUT)" >&2
      exit 2 ;;
  esac
  _ht="$HEALTH_TIMEOUT"
  while [[ "$_ht" == 0?* ]]; do _ht="${_ht#0}"; done
  if (( ${#_ht} > 10 || _ht > 2147483647 || _ht <= 0 )); then
    echo "simple-rebuild.sh: HEALTH_TIMEOUT must be a positive integer (got: $HEALTH_TIMEOUT)" >&2
    exit 2
  fi
  unset _ht
fi

agent_pid() {
  launchctl list | awk -v l="$1" '$3 == l && $1 != "-" { print $1 }'
}

agent_answers() {
  case "$1" in
    -) return 0 ;;
    cmd:*) (eval "${1#cmd:}") >/dev/null 2>&1 ;;
    *) [[ "$(curl -s -m 5 -o /dev/null -w '%{http_code}' "$1" || true)" != "000" ]] ;;
  esac
}

# What launchd knows (state, program, last exit code) plus the tail of the
# agent's own stderr log, which together name the cause in both failures
# above; the app log alone was empty for the first.
report_agent() {
  local label="$1" url="$2" info errlog
  print -P "%F{red}%B==> $label is not up%b%f"
  if ! info=$(launchctl print "gui/$(id -u)/$label" 2>&1); then
    echo "    not loaded in gui/$(id -u)"
    return 0
  fi
  grep -E $'^\t(state|program|last exit code) =' <<<"$info" | sed $'s/^\t/    /' || true
  case "$url" in
    -) ;;
    cmd:*) echo "    check failed: ${url#cmd:}" ;;
    *) echo "    $url did not answer" ;;
  esac
  errlog=$(awk -F' = ' $'/^\tstderr path = / { print $2; exit }' <<<"$info")
  if [[ -n "$errlog" && -r "$errlog" ]]; then
    echo "    last lines of $errlog:"
    tail -n 5 "$errlog" | sed 's/^/      /'
  fi
  echo "    Debugging launchd agents: docs/services.md (Debugging)"
}

check_services() {
  setopt local_options no_xtrace
  local entry label url pid failed=0
  local deadline=$(( SECONDS + ${HEALTH_TIMEOUT:-60} ))
  for entry in "${health_services[@]}"; do
    label="${entry%% *}" url="${entry#* }"
    while true; do
      pid=$(agent_pid "$label")
      if [[ -n "$pid" ]]; then
        sleep 3
        if [[ "$(agent_pid "$label")" == "$pid" ]] && agent_answers "$url"; then
          echo "up: $label (pid $pid)"
          break
        fi
      fi
      if (( SECONDS >= deadline )); then
        report_agent "$label" "$url" >&2
        failed=$(( failed + 1 ))
        break
      fi
      sleep 2
    done
  done
  if (( failed )); then
    print -P "%F{red}%B==> switch done and recorded, but $failed agent(s) are not up (above)%b%f" >&2
    return 1
  fi
}

phase_activate() {
  # Rolling back to an existing generation is a different operation from
  # activating a freshly built one, and deliberately does less: no rosetta
  # check, no ./result, and none of the post-activation steps below. Those all
  # run things *from the current flake* (brew bundle, the spotlight and git
  # helpers), which is precisely what you do not want when the point is to get
  # back to how the system was. `-G` sets action=rollback inside darwin-rebuild,
  # which reads $profile/systemConfig rather than building anything.
  if [[ -n "$generation" ]]; then
    # The darwin-rebuild that switches generations must come from the
    # installed system profile -- the profile --switch-generation operates
    # on -- not from ./result. ./result is a gitignored build artifact: it
    # may not exist (fresh clone, after a clean), and if it does it may be
    # the broken build being rolled back from. (Fixes #458.)
    local system_rebuild="/nix/var/nix/profiles/system/sw/bin/darwin-rebuild"
    if [[ ! -x "$system_rebuild" ]]; then
      echo "no darwin-rebuild in the system profile at $system_rebuild; cannot roll back" >&2
      return 1
    fi
    echo "darwin-rebuild --switch-generation $generation (rollback, as root)"
    sudo "$system_rebuild" --switch-generation "$generation"
    record_activation "rollback"
    return
  fi

  # Rosetta is best-effort, never a gate on the switch (#1069). This used to
  # run bare under `set -e`, before activation: on a clean install any
  # failure -- no network yet, softwareupdate not offering it, the reduced
  # Rosetta in macOS 27 -- aborted the run before the system was ever
  # activated. Nothing in the activation itself needs Rosetta; only the
  # x86_64 Homebrew prefix (nix-homebrew's enableRosetta) uses it later, and
  # that can be installed once the machine is up. Skip when it is already
  # installed (the runtime's presence is the installed marker) or not
  # Apple Silicon, and let a failed install warn and continue.
  if [[ "$(uname -m)" != arm64 ]]; then
    echo "not Apple Silicon; skipping Rosetta install"
  elif [[ -e /Library/Apple/usr/libexec/oah/libRosettaRuntime ]]; then
    echo "Rosetta already installed; skipping softwareupdate --install-rosetta"
  else
    echo "softwareupdate --install-rosetta --agree-to-license (best-effort)"
    softwareupdate --install-rosetta --agree-to-license ||
      echo "warning: Rosetta install did not complete; continuing with the switch -- rerun 'softwareupdate --install-rosetta --agree-to-license' once the machine is online for x86_64 Homebrew" >&2
  fi

  # Activate exactly the system phase_build made: register it as the next
  # system generation, then run its activation. This is what
  # `darwin-rebuild switch` does after its own build (nix-env --set, then
  # activate); `darwin-rebuild activate` reads the system to activate from
  # its own store path, so it is ./result's.
  #
  # This used to be `sudo darwin-rebuild switch --flake .`, which evaluates
  # the flake again as root. Root's evaluation cache is cold, so it needs
  # every flake input in the store, and flake input sources are not GC roots:
  # after a `nix store gc` it re-downloaded and unpacked nixpkgs (50,250
  # files, auto-optimise-store hardlinking each) and ran 27+ minutes before
  # being interrupted (2026-10-21). The same fetch as the user took 86 s. It
  # could also, in principle, activate something other than what was built
  # and committed if the tree changed in between.
  #
  # Registering the profile before activating also matters for GC: what is
  # activated is always a profile generation, so everything its activation
  # writes outside the store (mcpx's config.json, OpenChamber's links,
  # launchd plists) stays rooted. An activation of an unregistered build is
  # how mcpx lost its mcp-remote binary on 2026-10-21.
  if [[ -x ./result/sw/bin/darwin-rebuild ]]; then
    local system_config
    system_config=$(readlink -f ./result)
    echo "nix-env --set the system profile to $system_config, then darwin-rebuild activate (as root)"
    sudo /nix/var/nix/profiles/default/bin/nix-env -p /nix/var/nix/profiles/system --set "$system_config"
    # The activation's output is kept, and its start and end recorded in the
    # service event log: three switches on 2026-10-22 registered generations
    # 305-307 and never made them current, and nothing said where they
    # stopped (dezren39/nix#656, #670).
    local log_dir="$HOME/Library/Logs/nix-services" log
    mkdir -p "$log_dir"
    log="$log_dir/switch-$(date +%Y%m%dT%H%M%S).log"
    service_event switch.begin system="$system_config" \
      previous="$(readlink -f /run/current-system)" log="$log"
    if sudo "$system_config/sw/bin/darwin-rebuild" activate 2>&1 | tee "$log"; then
      service_event switch.activated system="$system_config"
    else
      local -a st=("${pipestatus[@]}")
      service_event switch.fail system="$system_config" exit="${st[1]}" log="$log"
      echo "darwin-rebuild activate failed (exit ${st[1]}); its output is in $log" >&2
      return 1
    fi
    # The profile can name a generation the activation never finished, so
    # check what is actually running, which activation links last.
    if [[ "$(readlink -f /run/current-system)" != "$system_config" ]]; then
      service_event switch.incomplete system="$system_config" \
        current="$(readlink -f /run/current-system)" log="$log"
      echo "activation exited 0 but /run/current-system is still $(readlink -f /run/current-system); see $log" >&2
      return 1
    fi
  else
    echo "no ./result to activate; run 'just build' first" >&2
    return 1
  fi

  brew_upgrade

  # The spotlight markers and the git system setup used to be re-run here, as
  # the user and again under sudo. All four calls were redundant:
  # configuration.nix's postActivation already runs both helpers, for root and
  # for the primary user, and `darwin-rebuild switch` above has just executed
  # it. See configuration.nix, `spotlight_exclude` / `git_maintain`.
  #
  # Removing them also closes a real hole rather than papering over it. Those
  # two helpers live in this repo, user-writable (-rwxr-xr-x drewry.pope) in a
  # user-writable directory, so `sudo ./git-maintain-repos` runs a file
  # anything with your uid can rewrite first. Activation instead invokes them
  # at their *store* paths -- ${./git-maintain-repos} -- which are root-owned
  # and read-only. That is also why neither is a candidate for a NOPASSWD
  # sudoers rule: a passwordless sudo on a user-writable script is a complete
  # root escalation, strictly worse than the password prompt it removes.

  record_activation "switch"

  # After recording, so a slow or failed App Store download can never delay
  # or cloud the record of a switch that has already happened.
  xcode_mas

  # Last, so the steps above still run and the generation that is now active
  # is recorded either way; an agent that is not up makes `just switch` exit
  # non-zero. Rollbacks (above) skip it: an older generation may not define
  # the same agents.
  check_services
}

# What the system actually is now, read from the profile symlink rather than
# from what we intended to do -- so a partial activation cannot be recorded as
# a successful one.
#
# Both reads are unprivileged, unlike `darwin-rebuild --list-generations`,
# which takes the profile lock and needs root:
#   readlink    /nix/var/nix/profiles/system  -> system-283-link  (the number)
#   readlink -f /nix/var/nix/profiles/system  -> /nix/store/...   (the closure)
# Append to the service event log (~/Library/Logs/nix-services/events.jsonl,
# dezren39/nix#670). Prefers the new build's service-event, since the running
# system may predate it; never fails the switch.
service_event() {
  local bin=./result/sw/bin/service-event
  [[ -x "$bin" ]] || bin=/run/current-system/sw/bin/service-event
  if [[ -x "$bin" ]]; then
    "$bin" rebuild "$@" || true
  fi
}

record_activation() {
  local how="$1" gen store
  gen=$(readlink /nix/var/nix/profiles/system 2>/dev/null || echo "system-?-link")
  gen="${${gen#system-}%-link}"
  store=$(basename "$(readlink -f /nix/var/nix/profiles/system 2>/dev/null || echo unknown)")
  echo "now: generation $gen -> $store"
  service_event "$how.recorded" generation="$gen" store="$store" \
    current="$(basename "$(readlink -f /run/current-system)")"
  reclaim_root_owned
  if (( git_ok )); then
    git add .
  else
    echo "no usable git: skipping git add after $how"
  fi
  commit_marker "$(hostname) $how gen $gen $store"
}

case "$mode" in
  build)    phase_build ;;
  activate) phase_activate ;;
  all)      phase_build && phase_activate ;;
esac
