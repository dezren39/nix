# SPDX-License-Identifier: MIT OR Apache-2.0
#
# mcpx module — shared options and config rendering (#343).
#
# Imported by every platform module (modules/mcpx/nixos.nix,
# modules/mcpx/home-manager.nix, and the darwin module when it lands):
# the option set is the contract from #343 — `services.mcpx.enable`,
# `package`, `settings` (rendered to config.json) and `servers` — and
# it is identical on every platform; only the service definition
# differs. No secret management, per the issue: credentials come from
# the environment the service runs in (`services.mcpx.environment`)
# or from files the user points at — mcpx's own `${VAR}` expansion in
# a server's `auth` reads those at runtime, nothing is rendered into
# the config beyond what the user wrote in `settings`/`servers`.
{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.services.mcpx;
in
{
  options.services.mcpx = {
    enable = lib.mkEnableOption "the mcpx daemon";

    package = lib.mkOption {
      type = lib.types.package;
      description = ''
        The mcpx package to run. The flake's module outputs default
        this to the flake's own `packages.<system>.mcpx` build (from
        the pinned mcpx-src input); override it to run another build.
      '';
    };

    settings = lib.mkOption {
      type = lib.types.attrsOf lib.types.anything;
      default = { };
      example = lib.literalExpression "{ daemon.autostart = false; }";
      description = ''
        mcpx settings, rendered verbatim into the daemon's
        config.json (see `services.mcpx.configFile`). `servers` are
        merged in as `mcpServers` and win over a `mcpServers` key
        written here.
      '';
    };

    servers = lib.mkOption {
      type = lib.types.attrsOf (lib.types.attrsOf lib.types.anything);
      default = { };
      example = lib.literalExpression ''
        {
          codedb = {
            command = "/run/current-system/sw/bin/codedb";
            args = [ "mcp" ];
            env.CODEDB_NO_TELEMETRY = "1";
          };
        }
      '';
      description = ''
        MCP servers the daemon pools, rendered as `mcpServers` in
        config.json. Each server's attributes pass through untouched
        (`command`, `args`, `env`, `auth`, pooling keys, ...). Use
        absolute paths for `command`: a daemon inherits the PATH of
        whatever started it, so a bare name can fail to resolve.
      '';
    };

    environment = lib.mkOption {
      type = lib.types.attrsOf lib.types.str;
      default = { };
      example = lib.literalExpression ''{ MCPX_STATE_DIR = "/var/lib/mcpx"; }'';
      description = ''
        Extra environment variables for the daemon process. This is
        the supported way to hand the daemon credentials: the values
        are the user's own, set in their own configuration — the
        module itself manages no secrets.
      '';
    };

    configFile = lib.mkOption {
      type = lib.types.path;
      readOnly = true;
      description = ''
        The rendered config.json (settings + servers) the daemon is
        started with. Read-only: derived from `settings` and
        `servers`.
      '';
    };
  };

  config = lib.mkIf cfg.enable {
    services.mcpx.configFile = pkgs.writeText "mcpx-config.json" (
      builtins.toJSON (cfg.settings // { mcpServers = cfg.servers; })
    );
  };
}
