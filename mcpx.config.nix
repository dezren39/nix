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
# datadog: US1 (mcp.datadoghq.com). Toolsets core (logs, metrics, traces,
# dashboards, monitors, incidents) and workflows; others are listed at
# https://docs.datadoghq.com/mcp_server/setup#toolsets. Changing the URL,
# toolsets included, is a new identity to mcp-remote and needs a new login.
{ lib }:
let
  lootbox = builtins.fromJSON (builtins.readFile ./lootbox.config.json);

  # How mcpx pools each server, where the default (one process, shared by
  # every caller: sharing "shared", scope "global") is wrong.
  pooling = {
    # A browser holds page state; two agents sharing one corrupt each other.
    # One per agent session instead.
    chrome-devtools = {
      sharing = "exclusive";
      scope = "session";
    };
  };
in
{
  mcpServers = lib.mapAttrs (
    name: server: server // lib.optionalAttrs (pooling ? ${name}) { mcpx = pooling.${name}; }
  ) lootbox.mcpServers;
}
