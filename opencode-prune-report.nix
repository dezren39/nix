# SPDX-License-Identifier: MIT OR Apache-2.0
#
# Scheduled DRY-RUN report for the opencode pruners (#659).
#
# Two pruners exist but nothing runs them: clean.sh's short-lived scratch
# sweep (~/.local/share/opencode/tmp, 8 h TTL, name-pattern gated) and
# opencode-db-backup-prune.sh (timestamp-named backup dirs plus the loose
# opencode.db.bak-* copies beside the DB). The 2026-10-22 measurement had the
# Mac's scratch store at 39 GB, and the 2026-10-19 reliability review found the
# disk at 99% -- both grew between manual runs because neither pruner was ever
# scheduled (repo-wide search, 2026-11-03: no launchd/systemd job runs
# clean.sh; the only StartCalendarInterval job is git-maintenance).
#
# This agent runs BOTH pruners once a day in DRY-RUN and writes the report to
# a log. It deliberately deletes nothing:
#
#   * clean.sh --dry-run: del() prints instead of removing; --dry-run also
#     forces USE_SUDO=0, so the job never prompts for or gains root. The
#     name-blind catch-all is reported, never pruned (--prune-unknown-scratch
#     is not passed), and generic scratch bulk stays a Drew decision after
#     #377 -- a scheduled deletion path here would be making that call for
#     him.
#   * opencode-db-backup-prune.sh: dry-run is its default; --apply is not
#     passed.
#
# --no-git-maintenance drops the slowest part of clean.sh (the repo-wide
# git-maintain pass); this job reports on disk, it does not maintain git.
#
# The first real (applied) run and the decision about deleting generic scratch
# belong on the Mac and to Drew, so the job definition landing here is the
# whole repo-side remainder of #659.
{ config, pkgs, ... }:
let
  user = config.system.primaryUser;
  home = config.users.users.${user}.home;

  # clean.sh and opencode-db-backup-prune.sh both source lib/common.sh from
  # their own directory, so they are copied together with lib/ -- the same
  # runCommandLocal wrap configuration.nix uses for spotlight-exclude-artifacts
  # and git-maintain-repos. A bare ./clean.sh in the store has no library
  # beside it and dies at source time.
  #
  # clean.sh also reaches its $SELF_DIR siblings, and the store copy must
  # carry every one of them or the report dies mid-run (#1436):
  #   executed directly (not via run()): git-discover-repos -- the
  #     --dry-run --no-git-maintenance report shape execs it at the
  #     discovery pass and exits with its status, so a missing copy was
  #     a fatal 127 that never reached the opencode scratch section;
  #     var-folders-clean.sh (the --tmpdir sweep helper; not reached in
  #     the report shape, which never sets CLEAN_TMPDIR).
  #   executed via run() (echo-only under --dry-run, and the report
  #     passes --no-git-maintenance besides): git-maintain-repos.
  #   guarded reads ([ -f ... ]; absence silently skips the section):
  #     casks.nix (brew cask list) and masApps.nix (mas app list).
  # The siblings source lib/common.sh themselves, already copied above.
  tools = pkgs.runCommandLocal "opencode-prune-report-tools" { } ''
    mkdir -p $out/lib
    cp ${./clean.sh} $out/clean.sh
    cp ${./opencode-db-backup-prune.sh} $out/opencode-db-backup-prune.sh
    cp ${./lib/common.sh} $out/lib/common.sh
    cp ${./git-discover-repos} $out/git-discover-repos
    cp ${./git-maintain-repos} $out/git-maintain-repos
    cp ${./var-folders-clean.sh} $out/var-folders-clean.sh
    cp ${./casks.nix} $out/casks.nix
    cp ${./masApps.nix} $out/masApps.nix
    chmod +x $out/clean.sh $out/opencode-db-backup-prune.sh $out/git-discover-repos $out/git-maintain-repos $out/var-folders-clean.sh
  '';

  report = pkgs.writeShellScript "opencode-prune-report" ''
    set -u
    log() { echo "$(/bin/date -u +%Y-%m-%dT%H:%M:%SZ) $*"; }
    rc=0
    log "=== opencode prune dry-run report ==="

    # 1. Scratch sweep. --dry-run is report-only and sets USE_SUDO=0; no
    # --prune-unknown-scratch, so the name-blind catch-all is listed, not
    # deleted (#377 left that call to Drew).
    log "-- clean.sh --dry-run --no-git-maintenance"
    ${pkgs.bash}/bin/bash "${tools}/clean.sh" --dry-run --no-git-maintenance || rc=1

    # 2. DB backup retention. Dry-run is the default; --apply is never passed.
    log "-- opencode-db-backup-prune.sh --db-dir (dry run)"
    ${pkgs.bash}/bin/bash "${tools}/opencode-db-backup-prune.sh" --db-dir "$HOME/.local/share/opencode" || rc=1

    log "=== report complete (rc=$rc) ==="
    exit "$rc"
  '';
in
{
  launchd.user.agents.opencode-prune-report.serviceConfig = {
    Label = "org.nixos.opencode-prune-report";
    ProgramArguments = [ "${report}" ];
    EnvironmentVariables.HOME = home;
    # Not at load: a login is not a reason to pay for a full clean.sh walk,
    # and the report is only useful at a stable time of day.
    RunAtLoad = false;
    # Daily at 04:15, after git-maintenance (03:30) has finished. One run a
    # day is enough to see growth trends in the log.
    StartCalendarInterval = [
      {
        Hour = 4;
        Minute = 15;
      }
    ];
    ProcessType = "Background";
    LowPriorityIO = true;
    StandardOutPath = "${home}/Library/Logs/opencode-prune-report.log";
    StandardErrorPath = "${home}/Library/Logs/opencode-prune-report.log";
  };
}
