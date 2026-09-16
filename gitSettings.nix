# SPDX-License-Identifier: MIT OR Apache-2.0
#
# gitSettings.nix — single source of truth for global git configuration.
#
# Imported by homePrograms.nix as `programs.git.settings`. This is the freeform
# gitconfig attrset in current home-manager: keys land directly in
# ~/.config/git/config.
#
# HISTORY / GOTCHA
#   These keys previously lived under `programs.git.settings.extraConfig`, which
#   emitted literal `[extraConfig "core"]` sections. Git ignores unknown
#   sections outright, so init.defaultBranch, core.editor, core.autocrlf,
#   core.bigFileThreshold and safe.directory were all silently inert. Keys must
#   sit directly under `settings`.
#
#   Note also that ~/.gitconfig (unmanaged, hand-written) is read AFTER
#   ~/.config/git/config, so anything it sets wins over this file. See
#   `just git-config-check`.
{
  user = {
    name = "Drewry Pope";
    email = "drewry.pope@vertexinc.com"; # TODO: move work email out of default
  };

  init.defaultBranch = "main";
  safe.directory = "*";

  core = {
    editor = "vim";
    autocrlf = "input";
    bigFileThreshold = "16m";
    # Built-in filesystem monitor daemon (git >= 2.37). Stops `git status`
    # rescanning the whole worktree. A short-lived daemon is spawned per repo
    # on demand and idles out again.
    fsmonitor = true;
    untrackedCache = true;
  };

  filter.lfs = {
    clean = "git-lfs clean -- %f";
    smudge = "git-lfs smudge -- %f";
    process = "git-lfs filter-process";
    required = true;
  };

  # index.version=4 + index.skipHash + untrackedCache. skipHash makes the index
  # unreadable by git < 2.40; this machine runs 2.55.
  feature.manyFiles = true;

  fetch = {
    prune = true; # drop remote-tracking refs deleted upstream
    parallel = 0; # auto-parallelise multi-remote/submodule fetches
    writeCommitGraph = true;
  };
  # fetch.pruneTags is deliberately NOT set: it would delete local-only tags
  # that never existed on the remote.

  push = {
    autoSetupRemote = true; # no more `--set-upstream` on first push
    followTags = true;
  };

  rebase = {
    autoSquash = true;
    autoStash = true;
    updateRefs = true; # 2.38+: keep stacked branches pointing correctly
  };

  # Migrated from the former unmanaged ~/.gitconfig, which set this and won by
  # precedence. Preserving observed behaviour; flip to false for merge-default.
  pull.rebase = true;

  # GitHub credential helpers, migrated from ~/.gitconfig. The leading empty
  # string resets any inherited helper list before appending gh's.
  credential = {
    "https://github.com".helper = [
      ""
      "!/run/current-system/sw/bin/gh auth git-credential"
    ];
    "https://gist.github.com".helper = [
      ""
      "!/run/current-system/sw/bin/gh auth git-credential"
    ];
  };

  merge.conflictStyle = "zdiff3"; # 2.35+, strictly better than diff3
  rerere = {
    enabled = true;
    autoUpdate = true;
  };

  diff = {
    algorithm = "histogram";
    colorMoved = "zebra";
    mnemonicPrefix = true;
    renames = "copies";
  };

  # difftastic (structural / syntax-aware diff) shorthands. `difft` ships with
  # the difftastic package. Kept as aliases rather than a global diff.external so
  # plain `git diff` stays line-based (safe for scripts / pipes / other tools).
  alias = {
    dft = "!GIT_EXTERNAL_DIFF=difft git diff"; # structural diff, working tree
    dfts = "!GIT_EXTERNAL_DIFF=difft git diff --staged"; # structural diff, staged
    dl = "!GIT_EXTERNAL_DIFF=difft git log -p --ext-diff"; # log with structural patches
    dshow = "!GIT_EXTERNAL_DIFF=difft git show --ext-diff"; # show a commit structurally
  };

  # Object-store performance. gc.auto is NOT disabled globally — it is set
  # per-repo by ./git-maintain-repos, so repos outside that script keep their
  # normal safety net.
  gc.writeCommitGraph = true;
  pack.writeReverseIndex = true;

  branch.sort = "-committerdate";
  tag.sort = "version:refname";
  column.ui = "auto";
  log.date = "iso";
  help.autoCorrect = "prompt";
  transfer.credentialsInUrl = "warn";
}
