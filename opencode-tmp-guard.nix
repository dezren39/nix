# SPDX-License-Identifier: MIT OR Apache-2.0
#
# Keep work out of reach of macOS's temp-directory sweep.
#
# com.apple.bsd.dirhelper runs daily at 03:35 (CLEAN_FILES_OLDER_THAN_DAYS=3)
# and deletes files in the per-user /private/var/folders/*/T that have not been
# ACCESSED for three days. Write-once, rarely-read files go first: a git repo's
# HEAD and config, a worktree's .git pointer file. On 2026-10-17 it broke 9
# clones and 2 worktrees that agents had put in $TMPDIR/opencode, because
# opencode advertises os.tmpdir()/opencode as approved scratch space
# (dezren39/nix#377). Its plist is under SIP and the same service creates the
# per-user temp directories, so it is worked around rather than disabled.
#
# The real fix is the opencode wrapper in flake.nix (TMPDIR defaults to
# ~/.local/share/opencode/tmp, so opencode's scratch becomes
# ~/.local/share/opencode/tmp/opencode). Until every opencode on the machine is
# that wrapper, $TMPDIR/opencode is a symlink to the durable directory, and
# this agent keeps it that way:
#
#   * symlink missing -> recreate it.
#   * a real directory reappeared in its place (something wrote there after the
#     link was swept) -> fold its contents into the durable directory, keeping
#     both copies on a name clash, then relink.
#   * symlink present -> refresh its access time, so the sweep never takes it.
#
# Every git repo or worktree still under $TMPDIR has its .git metadata (the
# folder, or a worktree's pointer file) refreshed on every run, whatever its
# age: it is small, and it is what the sweep takes first. Working files are
# refreshed only if something in the repo changed in the last 14 days, so
# abandoned test repos still age out; ordinary temp files are left to the
# sweep.
#
# clean.sh is unaffected: touch -a changes access time (and ctime) only, and
# every age test in clean.sh reads modification time (-mtime, -mmin, date -r,
# stat %y/%Y); git-maintain-repos and git-discover-repos use no timestamps.
{ config, pkgs, ... }:
let
  user = config.system.primaryUser;
  home = config.users.users.${user}.home;
  guard = pkgs.writeShellScript "opencode-tmp-guard" ''
    set -u
    log() { echo "$(/bin/date -u +%Y-%m-%dT%H:%M:%SZ) $*"; }
    T=$(/usr/bin/getconf DARWIN_USER_TEMP_DIR) || exit 1
    T=''${T%/}
    D="$HOME/.local/share/opencode/tmp/opencode"
    L="$T/opencode"
    /bin/mkdir -p "$D"

    if [ -L "$L" ]; then
      target=$(/usr/bin/readlink "$L")
      [ "$target" = "$D" ] || log "WARN $L points at $target, not $D; leaving it"
      /usr/bin/touch -a -h "$L"
    elif [ -d "$L" ]; then
      stamp=$(/bin/date -u +%Y%m%dT%H%M%SZ)
      for e in "$L"/* "$L"/.[!.]* "$L"/..?*; do
        [ -e "$e" ] || [ -L "$e" ] || continue
        n=''${e##*/}
        if [ -e "$D/$n" ] || [ -L "$D/$n" ]; then
          /bin/mv "$e" "$D/$n.from-tmpdir-$stamp"
        else
          /bin/mv "$e" "$D/"
        fi
      done
      if /bin/rmdir "$L" 2>/dev/null; then
        /bin/ln -s "$D" "$L" && log "relinked $L -> $D (folded a recreated directory into it)"
      else
        log "WARN $L still not empty (written during the move); next run retries"
      fi
    elif [ ! -e "$L" ]; then
      /bin/ln -s "$D" "$L" && log "created $L -> $D"
    fi

    repos=$(/usr/bin/find "$T" -maxdepth 4 -name .git -prune -print 2>/dev/null)

    # .git metadata, every repo, every run, regardless of age. It is tiny, and
    # it is what the sweep took first (HEAD, config, loose refs, info/exclude,
    # a worktree's .git pointer file); without it git no longer recognises the
    # repo at all. -h so a symlink's own atime is refreshed, not its target's.
    meta=0
    while IFS= read -r g; do
      [ -n "$g" ] || continue
      /usr/bin/find "$g" -exec /usr/bin/touch -a -h {} + 2>/dev/null
      meta=$((meta + 1))
    done <<EOF
    $repos
    EOF

    # Working files only for repos changed in the last 14 days, so abandoned
    # ones (test fixtures, finished scratch clones) still age out.
    refreshed=0
    while IFS= read -r g; do
      [ -n "$g" ] || continue
      r=''${g%/.git}
      [ -n "$(/usr/bin/find "$r" -mtime -14 -print 2>/dev/null | /usr/bin/head -1)" ] || continue
      /usr/bin/find "$r" -exec /usr/bin/touch -a -h {} + 2>/dev/null
      refreshed=$((refreshed + 1))
    done <<EOF
    $repos
    EOF
    [ "$meta" -eq 0 ] || log "refreshed .git metadata in $meta repo(s), working files in $refreshed (changed <14d), under $T"
  '';
in
{
  launchd.user.agents.opencode-tmp-guard.serviceConfig = {
    Label = "org.nixos.opencode-tmp-guard";
    ProgramArguments = [ "${guard}" ];
    EnvironmentVariables.HOME = home;
    RunAtLoad = true;
    # Every 6 hours: well inside dirhelper's 3-day access window.
    StartInterval = 21600;
    ProcessType = "Background";
    LowPriorityIO = true;
    StandardOutPath = "${home}/Library/Logs/opencode-tmp-guard.log";
    StandardErrorPath = "${home}/Library/Logs/opencode-tmp-guard.log";
  };
}
