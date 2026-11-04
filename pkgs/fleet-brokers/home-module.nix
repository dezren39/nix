{
  config,
  lib,
  pkgs,
  ...
}:

let
  cfg = config.services.fleet-brokers;
in
{
  options.services.fleet-brokers = {
    enable = lib.mkEnableOption "the laptop fleet services (laptop-agent + secrets-broker + queue-broker, code#5343/code#5362)";

    package = lib.mkOption {
      type = lib.types.package;
      description = ''
        The fleet-brokers package (bin/secrets-broker, bin/queue-broker,
        bin/repo-queue). No default: the enabling configuration supplies
        the flake's own package, so the module stays usable from any
        flake that imports it.
      '';
    };

    laptopAgentPackage = lib.mkOption {
      type = lib.types.package;
      description = ''
        The laptop-agent package (bin/laptop-agent): the resident
        execution agent the sandbox session proxy routes to
        (code#5343). No default, for the same reason as `package`.
      '';
    };
  };

  config = lib.mkIf (cfg.enable && pkgs.stdenv.hostPlatform.isLinux) {
    home.packages = [
      cfg.package
      cfg.laptopAgentPackage
    ];

    systemd.user.services.laptop-agent = {
      Unit = {
        Description = "laptop-agent — resident fleet execution agent (code#5343)";
        After = [ "network-online.target" ];
      };
      Service = {
        ExecStart = "${cfg.laptopAgentPackage}/bin/laptop-agent";
        Restart = "on-failure";
        RestartSec = 3;
        Environment = [
          "LAPTOP_AGENT_HOST=100.99.113.103"
          "LAPTOP_AGENT_PORT=4799"
        ];
      };
      Install.WantedBy = [ "default.target" ];
    };

    systemd.user.services.secrets-broker = {
      Unit = {
        Description = "secrets-broker — resident Bitwarden secrets cache (code#5362)";
        After = [ "network-online.target" ];
      };
      Service = {
        ExecStart = "${cfg.package}/bin/secrets-broker";
        Restart = "on-failure";
        RestartSec = 5;
      };
      Install.WantedBy = [ "default.target" ];
    };

    systemd.user.services.queue-broker = {
      Unit = {
        Description = "queue-broker — repo-health queue service (code#5362)";
        After = [
          "network-online.target"
          "secrets-broker.service"
        ];
        Wants = [ "secrets-broker.service" ];
      };
      Service = {
        ExecStart = "${cfg.package}/bin/queue-broker";
        Restart = "on-failure";
        RestartSec = 5;
      };
      Install.WantedBy = [ "default.target" ];
    };
  };
}
