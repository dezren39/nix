# SPDX-License-Identifier: MIT OR Apache-2.0
#
# mcpx module — NixOS (#343, slice 2).
#
# Installs a systemd *user* unit equivalent to the daemon definition
# this repo's darwin configuration runs by hand
# (configuration.nix, launchd.user.agents.mcpx):
#
# - started at login (wantedBy default.target) and kept running
#   (Restart = "always", launchd's KeepAlive);
# - TimeoutStopSec = 20: mcpx's stop can take up to 10 s (it drains
#   HTTP and stops its server processes, all bounded by
#   limits.execDrainTimeout), so systemd's default stop window would
#   cut every restart off mid-shutdown — the same reason the launchd
#   agent sets ExitTimeOut = 20;
# - WorkingDirectory = %h, on purpose: mcpx keys a daemon by the set
#   of config files it finds searching up from where it starts, and a
#   CLI run from $HOME finds the same set, so it uses this daemon
#   instead of starting its own;
# - `--config` is named explicitly rather than discovered: at login
#   the config may not be discoverable yet, and a named missing file
#   is an error the restart policy retries. The rendered config is a
#   store path, so the daemon's socket — which mcpx names after the
#   config file's resolved path — is one per config content; a config
#   change restarts the unit onto the new socket and CLIs from this
#   same configuration compute the same path.
{
  config,
  lib,
  ...
}:
let
  cfg = config.services.mcpx;
in
{
  imports = [ ./common.nix ];

  config = lib.mkIf cfg.enable {
    environment.systemPackages = [ cfg.package ];

    systemd.user.services.mcpx = {
      description = "mcpx daemon";
      wantedBy = [ "default.target" ];
      environment = cfg.environment;
      serviceConfig = {
        ExecStart = "${cfg.package}/bin/mcpx daemon --config ${cfg.configFile}";
        Restart = "always";
        TimeoutStopSec = 20;
        WorkingDirectory = "%h";
      };
    };
  };
}
