# SPDX-License-Identifier: MIT OR Apache-2.0
#
# mcpx's user config (~/.config/mcpx/config.json), derived from lootbox's so
# the two gateways front the same MCP servers from one list. Edit the servers
# in lootbox.config.json; only what differs for mcpx lives here.
#
# Plain data, no secrets: every server here runs locally or authenticates on
# its own. context7 and datadog go through mcp-remote, which runs the OAuth
# login in a browser and keeps the tokens in ~/.mcp-auth, so nothing secret
# is in this repo.
#
# This file is generated into the nix store, so it is read-only: `mcpx servers
# add` cannot write to it. Add mcpx-only servers here, or in a project's
# .mcpx.json.
#
# github: GitHub's local server with `gh`'s token (see github-mcp in
# systemPackages.nix). atlassian: Rovo MCP v2 through mcp-remote; ?tools=all
# lists every tool flat, which Atlassian recommends for gateways, instead of
# its own discover-then-execute pair.
#
# datadog: US1 (mcp.datadoghq.com), toolsets:
#   core      logs, metrics, traces, spans, RUM, monitors, incidents, hosts,
#             dashboards, notebooks. Writes: create/edit notebook, and
#             upsert_datadog_dashboard (create or overwrite a dashboard).
#   workflows find, inspect, validate, create, update, publish, run and
#             cancel workflows. Running one can do whatever the workflow does.
#   alerting  monitor templates, validation, coverage, SLO search, and
#             create_datadog_monitor -- which creates in draft: no
#             notifications, priority 5, published by hand in the UI. The MCP
#             server has no tool to update or delete a monitor at all.
# omit_tools removes delete_datadog_workflow, the only delete in these
# toolsets. omit_tools takes exact names, not patterns; a new toolset needs
# its own delete_* names added. All tools: https://docs.datadoghq.com/mcp_server/tools
# Changing the URL is a new identity to mcp-remote and needs `just mcp-login
# datadog` again.
{ lib }:
let
  lootbox = builtins.fromJSON (builtins.readFile ./lootbox.config.json);

  # npm-installed servers, by absolute path. A daemon inherits the PATH of
  # whatever started it: the launchd agent's includes this directory, but a
  # daemon the CLI starts -- from a project with its own .mcpx.json, or when
  # the agent is down -- has the shell's, which does not, and every
  # mcp-remote server failed with "executable file not found".
  npmBin = "/Users/drewry.pope/.local/share/lootbox/npm/node_modules/.bin";
  npm = [
    "mcp-remote"
    "chrome-devtools-mcp"
  ];
  absolute =
    server:
    server
    // lib.optionalAttrs (builtins.elem (server.command or "") npm) {
      command = "${npmBin}/${server.command}";
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
  mcpServers = lib.mapAttrs (
    name: server: absolute server // lib.optionalAttrs (pooling ? ${name}) { mcpx = pooling.${name}; }
  ) lootbox.mcpServers;
}
