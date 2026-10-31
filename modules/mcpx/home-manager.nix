# SPDX-License-Identifier: MIT OR Apache-2.0
#
# mcpx module — home-manager (#343).
#
# Same service semantics as the NixOS module (modules/mcpx/nixos.nix,
# whose header carries the reasoning): start at login, keep running,
# a 20 s stop window, $HOME as the working directory, and the
# rendered config named with `--config`. On Linux the unit is a
# systemd user service; on darwin it is a launchd user agent with
# the equivalent fields (KeepAlive / RunAtLoad / ExitTimeOut). The
# darwin branch is the home-manager rendering of the launchd agent
# configuration.nix runs today; the standalone `darwinModules.mcpx`
# output is the remaining slice of #343.
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
  imports = [ ./common.nix ];

  config = lib.mkIf cfg.enable (
    lib.mkMerge [
      { home.packages = [ cfg.package ]; }

      (lib.mkIf pkgs.stdenv.hostPlatform.isLinux {
        systemd.user.services.mcpx = {
          Unit.Description = "mcpx daemon";
          Install.WantedBy = [ "default.target" ];
          # Via the service-level `environment` attribute, not
          # Service.Environment: home-manager injects its own
          # Service.Environment (session variables) and the two
          # definitions do not merge.
          environment = cfg.environment;
          Service = {
            ExecStart = "${cfg.package}/bin/mcpx daemon --config ${cfg.configFile}";
            Restart = "always";
            TimeoutStopSec = 20;
            WorkingDirectory = "%h";
          };
        };
      })

      (lib.mkIf pkgs.stdenv.hostPlatform.isDarwin {
        launchd.agents.mcpx = {
          enable = true;
          config = {
            ProgramArguments = [
              "${cfg.package}/bin/mcpx"
              "daemon"
              "--config"
              "${cfg.configFile}"
            ];
            EnvironmentVariables = cfg.environment;
            KeepAlive = true;
            RunAtLoad = true;
            ExitTimeOut = 20;
            WorkingDirectory = config.home.homeDirectory;
          };
        };
      })
    ]
  );
}
