# SPDX-License-Identifier: MIT OR Apache-2.0
#
# OpenChamber as a standing local server, and the desktop app as its client.
#
# Ported from developing-today/code (home/common/default.nix), where the same
# model runs as systemd user units; launchd stands in for systemd here:
#
#   * `openchamber serve --foreground` runs as a launchd agent at login and is
#     kept alive. It manages one OpenCode (`opencode serve`) and adopts the
#     OpenCode a previous OpenChamber left running, so sessions survive an
#     OpenChamber restart or crash. AbandonProcessGroup stops launchd from
#     killing that OpenCode along with OpenChamber; SIGTERM (launchd's stop)
#     drains HTTP and deliberately leaves OpenCode up for the next start.
#
#   * OpenCode is reached through a link, ~/.local/state/openchamber/bin/opencode.
#     A switch that only changes OpenCode re-points the link and sends SIGHUP,
#     and OpenChamber hands its OpenCode over to the new binary in place. A
#     switch that changes OpenChamber itself restarts the agent. Which one
#     happens is decided by comparing the start script's store path with the
#     one recorded at the last deploy (the code repo's "deploy key").
#
#   * The agent's ProgramArguments name a stable link to the start script, not
#     the store path, so the plist does not change from one switch to the next
#     and nix-darwin never restarts the agent on its own; the activation below
#     decides between restart, reload and nothing.
#
# Differences from the systemd version, because launchd cannot do them:
#   * No socket activation (launchd's socket handoff is not the LISTEN_FDS
#     protocol OpenChamber speaks), so the port is briefly closed during a
#     restart. A reload (the common case) keeps it open.
#   * No self-handoff (OPENCHAMBER_SELF_HANDOFF=0). A self-handoff successor
#     outlives the process launchd tracks; launchd would then see its job exit
#     and, with KeepAlive, start a second server that collides on the port.
#     systemd avoids that with MAINPID, which has no launchd equivalent.
#
# The desktop app finds this server through OPENCHAMBER_SERVER_URL and
# OPENCHAMBER_SKIP_LOCAL_SERVER (launchd.user.envVariables in
# configuration.nix), so it never starts a server or an OpenCode of its own.
{
  config,
  lib,
  pkgs,
  inputs,
  system,
  ...
}:
let
  user = config.system.primaryUser;
  home = config.users.users.${user}.home;
  ocPkgs = inputs.self.packages.${system};
  label = "org.nixos.openchamber";
  port = 3000;
  state = "${home}/.local/state/openchamber";

  # What OpenChamber gets as `opencode`: the deploy link, not a store path.
  # Keeps the OpenChamber build independent of the OpenCode build, so a new
  # OpenCode is a reload and not a restart (see above).
  opencodeLink = pkgs.runCommandLocal "opencode-deploy-link" { } ''
    mkdir -p $out/bin
    ln -s ${state}/bin/opencode $out/bin/opencode
  '';
  openchamber = inputs.openchamber.packages.${system}.openchamber.override {
    opencode = opencodeLink;
  };
  # The app's bundled opencode-cli is unused while it attaches to the agent's
  # server; the override only keeps it pointing at the same deploy link.
  openchamber-desktop = inputs.openchamber.packages.${system}.openchamber-desktop.override {
    opencode = opencodeLink;
  };

  start = pkgs.writeShellScript "openchamber-serve" ''
    # Optional: OpenChamber warns that the browser UI is unsecured without
    # one. Loopback-only either way. Kept out of the store; create with
    #   install -m600 /dev/null ~/.config/openchamber/ui-password
    pw="$HOME/.config/openchamber/ui-password"
    if [ -r "$pw" ]; then
      OPENCHAMBER_UI_PASSWORD="$(< "$pw")"
      export OPENCHAMBER_UI_PASSWORD
    fi
    export OPENCODE_BINARY="${state}/bin/opencode"
    export OPENCODE_DB="$HOME/.local/share/opencode/opencode.db"
    export OPENCHAMBER_SELF_HANDOFF=0
    # OpenChamber's terminals and git run with this PATH, so it is the full
    # system path, with the OpenCode link and mcpx ahead of it.
    export PATH="${state}/bin:${
      lib.makeBinPath [
        ocPkgs.mcpx
        pkgs.git
        pkgs.openssh
      ]
    }:${config.environment.systemPath}"
    exec ${openchamber}/bin/openchamber serve --foreground \
      --port ${toString port} --host 127.0.0.1
  '';
in
{
  # nix-darwin links it into /Applications/Nix Apps; mac-app-util adds the
  # Launchpad/Spotlight trampoline. It reads OPENCHAMBER_SERVER_URL and
  # OPENCHAMBER_SKIP_LOCAL_SERVER from launchd.user.envVariables, attaches to
  # the agent below as a local (desktop-local) client, and starts no server or
  # OpenCode of its own. Its "open at login" setting is unnecessary: the
  # agent is what must run at login, and the app is only a window onto it.
  environment.systemPackages = [ openchamber-desktop ];

  launchd.user.agents.openchamber.serviceConfig = {
    Label = label;
    ProgramArguments = [ "${state}/start" ];
    EnvironmentVariables.HOME = home;
    RunAtLoad = true;
    KeepAlive = true;
    # Until the first activation has created the start link the job fails
    # immediately; retry every few seconds rather than launchd's default 10.
    ThrottleInterval = 5;
    AbandonProcessGroup = true;
    ProcessType = "Interactive";
    StandardOutPath = "${home}/Library/Logs/openchamber.log";
    StandardErrorPath = "${home}/Library/Logs/openchamber.log";
  };

  # A function module, so `lib` is home-manager's (lib.hm is not in nix-darwin's).
  home-manager.users.${user} =
    { lib, ... }:
    {
      home.activation.openchamberDeploy = lib.hm.dag.entryAfter [ "writeBoundary" ] ''
        state='${state}'
        job="gui/$(/usr/bin/id -u)/${label}"
        key='${start}'
        target='${ocPkgs.opencode}/bin/opencode'
        previous="$(/usr/bin/readlink "$state/bin/opencode" 2>/dev/null || true)"
        running=0
        if /bin/launchctl print "$job" 2>/dev/null | /usr/bin/grep -q 'state = running'; then
          running=1
        fi
        run /bin/mkdir -p "$state/bin"
        run /bin/ln -sfn "$target" "$state/bin/opencode"
        run /bin/ln -sfn "$key" "$state/start"
        if [ "$(/bin/cat "$state/deployed" 2>/dev/null)" != "$key" ]; then
          # New OpenChamber (or start script): restart. SIGTERM leaves the
          # managed OpenCode running and the new process adopts it.
          if [ "$running" = 1 ]; then
            run /bin/launchctl kickstart -k "$job"
          else
            run /bin/launchctl kickstart "$job" || true
          fi
          run /bin/sh -c 'printf "%s\n" "$1" > "$2"' _ "$key" "$state/deployed"
        elif [ "$running" = 1 ] && [ "$previous" != "$target" ]; then
          # Same OpenChamber, new OpenCode: hand over in place.
          run /bin/launchctl kill SIGHUP "$job"
        fi
      '';
    };
}
