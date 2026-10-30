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
#     systemd's MAINPID adoption has no launchd equivalent -- but it is not
#     sufficient there either: with KillMode=process, a predecessor systemd
#     has stopped tracking can survive a stop still holding the
#     socket-activated listen socket, and the next start hands the same
#     socket to a second server (dezren39/nix#1117, 2026-10-29). The systemd
#     model therefore also reaps leftover servers in its start script
#     (developing-today/code#2465).
#
# The desktop app finds this server through OPENCHAMBER_SERVER_URL and
# OPENCHAMBER_SKIP_LOCAL_SERVER, so it never starts a server or an OpenCode of
# its own. It reads them from the login shell (environment.variables in
# configuration.nix); launchd.user.envVariables alone is lost at reboot.
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
  # server, but the desktop package still `ln -s`s it into the app bundle at
  # build time (nix/desktop-darwin.nix: opencode-cli/opencode), and nix-darwin's
  # system activation resolves that symlink with rsync --copy-unsafe-links
  # before home-manager's activation has ever pointed opencodeLink's target
  # (${state}/bin/opencode) at anything -- a dangling symlink there aborts the
  # whole `/Applications/Nix Apps` rsync (rsync: "symlink has no referent",
  # exit 23) on any machine where that state link does not already exist, e.g.
  # a first activation or one after `~/.local/state/openchamber` was cleared.
  # The real opencode package is always a real store path, so bundling it
  # instead sidesteps the ordering hazard entirely; which build it is does
  # not matter since the desktop app never runs it.
  openchamber-desktop = inputs.openchamber.packages.${system}.openchamber-desktop.override {
    opencode = ocPkgs.opencode;
  };

  start = pkgs.writeShellScript "openchamber-serve" ''
    # The service event log (dezren39/nix#670). By its stable profile path:
    # a store path here would make every service-events change restart
    # OpenChamber (see PATH below).
    if [ -x /run/current-system/sw/bin/service-event ]; then
      /run/current-system/sw/bin/service-event openchamber-agent agent.start pid=$$ \
        openchamber=${openchamber} || true
    fi
    # OPENCHAMBER_UI_PASSWORD, from sops (see home-manager.users below):
    # required before OpenChamber will serve a public tunnel. Loopback-only
    # either way, so this is defence in depth, not what keeps it private.
    pw="$HOME/.config/openchamber/ui-password"
    if [ -r "$pw" ]; then
      OPENCHAMBER_UI_PASSWORD="$(< "$pw")"
      export OPENCHAMBER_UI_PASSWORD
    fi
    export OPENCODE_BINARY="${state}/bin/opencode"
    export OPENCODE_DB="$HOME/.local/share/opencode/opencode.db"
    export OPENCHAMBER_SELF_HANDOFF=0
    # OpenChamber's terminals and git run with this PATH, so it is the full
    # system path, with the OpenCode link and the system profile's bin (mcpx,
    # git, openssh) ahead of it. The system profile, not store paths: this
    # script's store path is OpenChamber's deploy key, so naming
    # mcpx/git/openssh by store path restarted OpenChamber, and dropped every
    # client connection, whenever any of them changed (dezren39/nix#663).
    export PATH="${state}/bin:/run/current-system/sw/bin:${config.environment.systemPath}"
    exec ${openchamber}/bin/openchamber serve --foreground \
      --port ${toString port} --host 127.0.0.1
  '';
in
{
  # nix-darwin links it into /Applications/Nix Apps; mac-app-util adds the
  # Launchpad/Spotlight trampoline. It reads OPENCHAMBER_SERVER_URL and
  # OPENCHAMBER_SKIP_LOCAL_SERVER from the login shell, attaches to
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
    # OpenChamber drains for up to 10 s on SIGTERM (server/index.js), but
    # launchd's default here is 5 s before SIGKILL, which cut every restart
    # short. OpenCode is unaffected either way (AbandonProcessGroup).
    ExitTimeOut = 15;
    ProcessType = "Interactive";
    StandardOutPath = "${home}/Library/Logs/openchamber.log";
    StandardErrorPath = "${home}/Library/Logs/openchamber.log";
  };

  # A function module, so `lib` is home-manager's (lib.hm is not in nix-darwin's).
  home-manager.users.${user} =
    { lib, config, ... }:
    {
      # Decrypted by sops-nix's own launchd agent (org.nix-community.home.sops-nix,
      # RunAtLoad, rebootstrapped on every activation below) straight to the path
      # the start script above reads -- no activation wiring of our own needed for
      # the decrypt itself. The age identity is generated and kept out of the repo
      # (see .sops.yaml's comment); only the encrypted secrets/openchamber.yaml is
      # tracked.
      #
      # Reading the password back (to sign in, or to store it in a password
      # manager): it has no trailing newline, so printing it in zsh leaves a
      # reverse-video `%` after it -- zsh's PROMPT_EOL_MARK for an unterminated
      # line, not part of the password (the stored value ends in neither `%`
      # nor a newline; checked 2026-10-19 after a sign-in only worked with the
      # `%` dropped). Copy it without going through the terminal instead:
      #   sops -d --extract '["openchamber_ui_password"]' secrets/openchamber.yaml | pbcopy
      # The start script reads it with "$(< "$pw")", which keeps every byte
      # except trailing newlines, so a newline added when rotating it is
      # harmless; a stray `%` copied from the terminal is not.
      sops = {
        age.keyFile = "${config.home.homeDirectory}/Library/Application Support/sops/age/keys.txt";
        defaultSopsFile = ./secrets/openchamber.yaml;
        secrets.openchamber_ui_password = {
          path = "${home}/.config/openchamber/ui-password";
          mode = "0600";
        };
      };

      # sops-nix's own activation entry (modules/home-manager/sops.nix) does
      # `launchctl bootout` (tolerated: `&& true`) then `launchctl bootstrap`
      # with no failure tolerance, and no ordering against home-manager's own
      # `setupLaunchAgents` (which is what actually writes
      # ~/Library/LaunchAgents/org.nix-community.home.sops-nix.plist). On a
      # from-scratch activation -- this secret's first one, or any time the
      # plist did not already exist -- bootstrap can run before the plist is
      # on disk and fail with an I/O error, which aborted the whole
      # `darwin-rebuild switch` (exit 5), not just the secret decrypt
      # (nix-community/home-manager#6536 is the same symptom). Override with
      # the ordering and the same `|| true` tolerance bootout already gets:
      # the decrypt itself does not depend on this manual re-bootstrap
      # succeeding, since a first-time agent is already loaded by
      # setupLaunchAgents's own RunAtLoad.
      home.activation.sops-nix = lib.mkForce (
        lib.hm.dag.entryAfter [ "writeBoundary" "setupLaunchAgents" ] (
          let
            domainTarget = "gui/$(/usr/bin/id -u ${config.home.username})";
          in
          ''
            /bin/launchctl bootout ${domainTarget}/org.nix-community.home.sops-nix 2>/dev/null || true
            /bin/launchctl bootstrap ${domainTarget} ${config.home.homeDirectory}/Library/LaunchAgents/org.nix-community.home.sops-nix.plist || true
          ''
        )
      );

      home.activation.openchamberDeploy = lib.hm.dag.entryAfter [ "writeBoundary" "sops-nix" ] ''
        state='${state}'
        job="gui/$(/usr/bin/id -u)/${label}"
        pw="$HOME/.config/openchamber/ui-password"
        # sops-nix's own activation entry (just above us) only *starts* its
        # launchd agent (RunAtLoad, async); it does not wait for the
        # decrypt to finish. Poll briefly rather than hashing a file that
        # may not exist yet on a from-scratch activation.
        waited=0
        while [ ! -r "$pw" ] && [ "$waited" -lt 50 ]; do
          /bin/sleep 0.1
          waited=$((waited + 1))
        done
        # Folds the password's content into the deploy key so a password
        # rotation (secrets/openchamber.yaml changing, with nothing else
        # about OpenChamber touched) gets the same full restart as a new
        # OpenChamber build, instead of silently sitting unread until some
        # unrelated change happens to restart the agent too.
        #
        # The key is only ever compared with $state/deployed. The start link
        # must name the script alone: when the hash was added it was still
        # linked to "$key", i.e. "<script>:<hash>", a file that does not
        # exist, so launchd failed every spawn with exit 78 (EX_CONFIG) and
        # the server stayed down until the link was repaired (2026-10-19).
        script='${start}'
        key="$script:$(/usr/bin/shasum -a 256 "$pw" 2>/dev/null | /usr/bin/awk '{print $1}')"
        target='${ocPkgs.opencode}/bin/opencode'
        previous="$(/usr/bin/readlink "$state/bin/opencode" 2>/dev/null || true)"
        running=0
        if /bin/launchctl print "$job" 2>/dev/null | /usr/bin/grep -q 'state = running'; then
          running=1
        fi
        run /bin/mkdir -p "$state/bin"
        run /bin/ln -sfn "$target" "$state/bin/opencode"
        run /bin/ln -sfn "$script" "$state/start"
        # launchd only reports a bad link as a respawn loop with exit 78 and
        # nothing in openchamber.log, so check it here, where the cause is.
        if [[ ! -v DRY_RUN ]] && [ ! -x "$state/start" ]; then
          echo "openchamberDeploy: $state/start -> $(/usr/bin/readlink "$state/start") is not an executable; launchd cannot start OpenChamber" >&2
          exit 1
        fi
        # Recorded in the service event log (dezren39/nix#670): what this
        # switch decided and why. Names which part of the key changed, never
        # the password or its hash.
        event() {
          [[ -v DRY_RUN ]] || ${ocPkgs.service-events}/bin/service-event openchamber-deploy "$@" || true
        }
        deployed="$(/bin/cat "$state/deployed" 2>/dev/null || true)"
        if [ -z "$deployed" ]; then
          why=first-deploy
        elif [ "''${deployed%%:*}" != "$script" ]; then
          why=start-script-changed
        else
          why=ui-password-changed
        fi
        if [ "$deployed" != "$key" ]; then
          # New OpenChamber (or start script, or UI password): restart.
          # SIGTERM leaves the managed OpenCode running and the new process
          # adopts it.
          event deploy.restart cause="$why" running="$running" \
            previous_script="''${deployed%%:*}" script="$script"
          if [ "$running" = 1 ]; then
            run /bin/launchctl kickstart -k "$job"
          else
            run /bin/launchctl kickstart "$job" || true
          fi
          run /bin/sh -c 'printf "%s\n" "$1" > "$2"' _ "$key" "$state/deployed"
        elif [ "$running" = 1 ] && [ "$previous" != "$target" ]; then
          # Same OpenChamber, new OpenCode: hand over in place.
          event deploy.opencode-handover cause=opencode-changed previous="$previous" opencode="$target"
          run /bin/launchctl kill SIGHUP "$job"
        else
          event deploy.unchanged running="$running"
        fi
      '';
    };
}
