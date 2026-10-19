# SPDX-License-Identifier: MIT OR Apache-2.0
#
# mcpx's user config (~/.config/mcpx/config.json, written by homeUser.nix):
# every MCP server this machine runs, and how mcpx pools each one.
#
# Plain data, no secrets: every server here runs locally or authenticates on
# its own. context7, datadog and atlassian go through mcp-remote, which runs
# the OAuth login in a browser (`just mcp-login <name>`) and keeps the tokens
# in ~/.mcp-auth, so nothing secret is in this repo.
#
# This file is generated into the nix store, so it is read-only: `mcpx servers
# add` cannot write to it. Add servers here, or in a project's .mcpx.json.
#
# Commands are absolute paths: a daemon inherits the PATH of whatever started
# it, and a bare name that the launchd agent's PATH resolves can fail with
# "executable file not found" under any other PATH. The npm-only servers
# (mcp-remote, chrome-devtools-mcp) come from pkgs/npm-mcp-servers.
#
# github: GitHub's local server with `gh`'s token (see github-mcp in
# systemPackages.nix). aws: AWS's managed MCP server through
# mcp-proxy-for-aws, which signs with SigV4 from the default AWS profile
# (`aws configure sso` then `aws sso login`); without credentials it fails
# when called and the other servers are unaffected. aws-knowledge is AWS's
# credential-free docs server; AWS recommends dropping it once aws works,
# since aws has the same documentation tools. atlassian: Rovo MCP v2
# through mcp-remote; ?tools=all lists every tool flat, which Atlassian
# recommends for gateways, instead of its own discover-then-execute pair.
#
# datadog: US1 (mcp.datadoghq.com), toolsets:
#   core      logs, metrics, traces, spans, RUM, monitors, incidents, hosts,
#             dashboards, notebooks. Writes: create/edit notebook, and
#             upsert_datadog_dashboard (create or overwrite a dashboard).
#   workflows find, inspect, validate, create, update, publish, run and
#             cancel workflows. Running one can do whatever the workflow does.
#   alerting  monitor templates, validation, coverage, SLO search,
#             create_datadog_monitor (creates in draft: no notifications,
#             priority 5, published by hand in the UI) and
#             update_datadog_monitor.
# omit_tools removes delete_datadog_workflow. delete_datadog_notebook (core)
# and update_datadog_monitor appeared later and are still exposed (checked
# with `mcpx types datadog`, 2026-10-19). omit_tools takes exact names, not
# patterns; a new toolset needs its own delete_* names added. All tools:
# https://docs.datadoghq.com/mcp_server/tools
# Changing the URL is a new identity to mcp-remote and needs `just mcp-login
# datadog` again.
{ lib }:
let
  bin = name: "/run/current-system/sw/bin/${name}";

  # fff indexes one root per process, so each searchable tree is its own
  # server; they share one frecency and history database.
  fff = root: {
    command = bin "fff-mcp";
    args = [
      "--no-update-check"
      "--frecency-db"
      "/Users/drewry.pope/.local/share/fff/frecency.db"
      "--history-db"
      "/Users/drewry.pope/.local/share/fff/history.db"
      root
    ];
  };

  remote = url: {
    command = bin "mcp-remote";
    args = [ url ];
  };

  servers = {
    codebase-memory = {
      command = bin "codebase-memory-mcp";
      args = [ ];
    };
    codedb = {
      command = bin "codedb";
      args = [ "mcp" ];
      env.CODEDB_NO_TELEMETRY = "1";
    };
    fff = fff "/Users/drewry.pope/git";
    fff-nix = fff "/Users/drewry.pope/.config/nix";
    fff-worktree = fff "/Users/drewry.pope/.local/share/opencode/worktree";
    chrome-devtools = {
      command = bin "chrome-devtools-mcp";
      args = [
        "--headless"
        "--isolated"
        "--viewport=800x600"
        "--no-usage-statistics"
        "--no-performance-crux"
        "--no-category-emulation"
      ];
    };
    context7 = remote "https://mcp.context7.com/mcp";
    datadog = remote "https://mcp.datadoghq.com/v1/mcp?toolsets=core,workflows,alerting&omit_tools=delete_datadog_workflow";
    atlassian = remote "https://mcp.atlassian.com/v2/mcp?tools=all";
    github = {
      command = bin "github-mcp";
      args = [ ];
    };
    aws = {
      command = bin "uvx";
      args = [
        "mcp-proxy-for-aws-cli@1.7.0"
        "https://aws-mcp.us-east-1.api.aws/mcp"
        "--metadata"
        "AWS_REGION=us-east-1"
      ];
    };
    aws-knowledge = {
      transport = "streamable_http";
      url = "https://knowledge-mcp.global.api.aws";
    };
    deepwiki = {
      transport = "streamable_http";
      url = "https://mcp.deepwiki.com/mcp";
    };
    terraform = {
      command = bin "terraform-mcp-server";
      args = [
        "stdio"
        "--toolsets=registry"
      ];
    };
    nixos = {
      command = bin "mcp-nixos";
      args = [ ];
    };
    playwright = {
      command = bin "playwright-mcp";
      args = [
        "--headless"
        "--isolated"
        "--viewport-size=1280x720"
        "--output-dir"
        "/Users/drewry.pope/.cache/playwright-mcp"
      ];
    };
    markitdown = {
      command = bin "markitdown-mcp";
      args = [ ];
    };
    sequential-thinking = {
      command = bin "mcp-server-sequential-thinking";
      args = [ ];
      env.DISABLE_THOUGHT_LOGGING = "true";
    };
  };

  # How mcpx pools each server, where the default (one process, shared by
  # every caller: sharing "shared", scope "global") is wrong.
  pooling = {
    # A browser holds page state; two agents sharing one corrupt each other.
    # One per agent session instead.
    chrome-devtools = {
      sharing = "exclusive";
      scope = "session";
    };
    playwright = {
      sharing = "exclusive";
      scope = "session";
    };
    # uvx resolves and downloads the proxy on first start, which can outlast
    # the default 60s start timeout.
    aws.startTimeout = "180s";
    # Not pooling, but the same per-server mcpx block. Hidden from listings
    # and refused if called by name. create_repository can make a public
    # repository (private: false), or one in the personal account when
    # organization is omitted; a fork of a public repository is public;
    # delete_repository is irreversible (and needs a delete_repo scope the
    # gh token lacks today). Kept: merge_pull_request, push_files and the
    # rest -- branch protection still applies, and no tool force-pushes
    # (both ref updates hard-code Force: false in github-mcp-server).
    # Re-allowing create_repository for private repositories only is #355.
    github.excludeTools = [
      "create_repository"
      "fork_repository"
      "delete_repository"
    ];
  };
in
{
  # Only the launchd agent (org.nixos.mcpx, configuration.nix) starts a
  # daemon. With autostart on, any mcpx command that found no daemon -- during
  # the seconds a switch restarts the agent, say -- started its own detached
  # one with a 4h idle timer; it kept the socket, so launchd's daemon exited
  # "a daemon is already listening" and was respawned every ~10 s, 822 times
  # on 2026-10-19, while clients ran on the stray's older build. Off, a
  # command with no daemon fails ("disabled by daemon.autostart") and works
  # again once the agent is back. Checked on a scratch state dir: read from
  # this file, no daemon spawned, commands still reach a running one.
  daemon.autostart = false;

  mcpServers = lib.mapAttrs (
    name: server: server // lib.optionalAttrs (pooling ? ${name}) { mcpx = pooling.${name}; }
  ) servers;
}
