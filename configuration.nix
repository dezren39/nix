# https://github.com/dustinlyons/nixos-config/blob/main/modules/darwin
# https://github.com/dustinlyons/nixos-config/blob/main/modules/darwin/dock/default.nix
#
{
  config,
  pkgs,
  lib,
  inputs,
  system,
  ...
}:
lib.recursiveUpdate {
  imports = [
    ./systemPackages.nix
    ./brews.nix
    ./casks.nix
    ./masApps.nix
    ./services.nix
    # ./home.nix
  ];

  # =========================================================================
  # System-wide git safety — applies to ALL users, ALL git binaries
  # =========================================================================

  # Patterns file at /etc/gitignore
  environment.etc."gitignore".text = ''
    # codedb snapshots — never commit anywhere
    codedb.snapshot

    # lootbox ephemeral dirs (scripts are fine)
    .lootbox/cache/
    .lootbox/tmp/
  '';

  # System gitconfig at /etc/gitconfig, generated from the SAME single source of
  # truth as the per-user config (gitSettings.nix, imported by homePrograms.nix
  # as programs.git.settings). Putting it at system scope means every git
  # invocation picks it up — the nix git, Apple's /usr/bin/git, and root during
  # activation or clean.sh — not just this user's interactive shells.
  # System scope is lowest precedence, so the user config still wins on conflict.
  # One generated file, referenced twice. /etc/gitconfig is what git reads via
  # GIT_CONFIG_SYSTEM; /var/root/.gitconfig is what root reads when sudo has
  # stripped that variable. Symlinking both at the same store path means they
  # cannot drift, and nothing is copied at activation time.
  environment.etc."gitconfig".source = pkgs.writeText "gitconfig" (
    lib.generators.toGitINI (
      lib.recursiveUpdate (import ./gitSettings.nix) {
        core.excludesFile = "/etc/gitignore";
        # System scope is read by ROOT (activation, the git-maintenance daemon,
        # clean.sh, `sudo git`). When root runs git with core.fsmonitor=true it
        # spawns the fsmonitor daemon and leaves root-owned cookie files in every
        # repo, which then block `rm` (repo deletion needs sudo). Disable it for
        # the system/root config only — the user's global ~/.config/git/config
        # still sets core.fsmonitor = true and wins by precedence, so interactive
        # user git keeps the fast daemon.
        core.fsmonitor = false;
      }
    )
  );

  # Force nix-packaged git to read /etc/gitconfig (it normally reads $nixStore/etc/gitconfig)
  environment.variables.GIT_CONFIG_SYSTEM = "/etc/gitconfig"; # List packages installed in system profile. To search by name, run:
  # $ nix-env -qaP | grep wget

  # opencode clamps output tokens to OUTPUT_TOKEN_MAX = 32_000
  # (provider/transform.ts:18), which is half what Opus 5 actually supports.
  # Thinking counts against max_tokens, so at high effort that makes
  # stop_reason: "max_tokens" twice as likely as it needs to be.
  #
  # The value is applied as Math.min(model.limit.output, this), so an absurdly
  # large number simply means "every model gets its own native ceiling" —
  # 64k for Opus 5, 128k for the gpt-5.x family — with no way to exceed it.
  #
  # Note this does NOT disturb the compaction reserve: that is
  # `Math.min(COMPACTION_BUFFER, maxOutputTokens(...))` (session/overflow.ts:14),
  # so it stays pinned at 20_000 either way.
  environment.variables.OPENCODE_EXPERIMENTAL_OUTPUT_TOKEN_MAX = "999999999";

  nixpkgs = {
    # TODO: module nixpkgs
    hostPlatform = "aarch64-darwin";
    config = {
      allowUnfree = true;
      #cudaSupport = true;
      #cudaCapabilities = ["8.0"];
      allowBroken = true;
      allowInsecure = false;
      allowUnsupportedSystem = true;
    };
    overlays = [
      inputs.fenix.overlays.default
      (final: prev: {
        alt-tab-debug = final.callPackage ./pkgs/alt-tab-debug.nix { };
        noTunes = final.callPackage ./pkgs/noTunes.nix { };
      })
      # Work around cctools ld crashing until nixpkgs#536365 reaches unstable.
      (final: prev: {
        cargo-watch = prev.cargo-watch.overrideAttrs (old: {
          nativeBuildInputs = (old.nativeBuildInputs or [ ]) ++ [ final.llvmPackages.lld ];
          env = (old.env or { }) // {
            NIX_CFLAGS_LINK = "-fuse-ld=${lib.getExe' final.llvmPackages.lld "ld64.lld"}";
          };
        });
        sketchybar = prev.sketchybar.overrideAttrs (old: {
          nativeBuildInputs = (old.nativeBuildInputs or [ ]) ++ [ final.llvmPackages.lld ];
          env = (old.env or { }) // {
            NIX_CFLAGS_LINK = "-fuse-ld=${lib.getExe' final.llvmPackages.lld "ld64.lld"}";
          };
        });
      })
      # (final: prev: {
      #   helium =
      #     (import inputs.nixpkgs-helium {
      #       inherit (prev) system;
      #       config = prev.config;
      #     }).helium;
      # })
    ];
  };

  # nix.package = pkgs.nix # disabled because using determinate nix

  # TODO: module system
  system = {
    configurationRevision = inputs.self.rev or inputs.self.dirtyRev or null; # Set Git commit hash for darwin-version.
    stateVersion = 5;
    defaults = {
      LaunchServices = {
        LSQuarantine = false;
      };

      NSGlobalDomain = {
        AppleShowAllExtensions = true;
        ApplePressAndHoldEnabled = false;

        # 120, 90, 60, 30, 12, 6, 2
        KeyRepeat = 2;

        # 120, 94, 68, 35, 25, 15
        InitialKeyRepeat = 15;

        "com.apple.mouse.tapBehavior" = 1;
        "com.apple.sound.beep.volume" = 0.0;
        "com.apple.sound.beep.feedback" = 0;

        # Auto-hide menu bar on desktop — reclaims notch row, shows on hover
        _HIHideMenuBar = true;
      };

      # dock = {
      # https://github.com/dustinlyons/nixos-config/blob/main/modules/darwin/home-manager.nix#L70
      #   autohide = true;
      #   show-recents = true;
      #   launchanim = true;
      #   mouse-over-hilite-stack = true;
      #   orientation = "bottom";
      #   tilesize = 48;
      # };

      finder = {
        _FXShowPosixPathInTitle = false;
      };

      trackpad = {
        Clicking = true;
        TrackpadThreeFingerDrag = true;
      };

      # =====================================================================
      # Spotlight — trim search categories and disable noisy result types
      # =====================================================================
      CustomUserPreferences = {
        "com.apple.Spotlight" = {
          orderedItems = [
            {
              enabled = true;
              name = "APPLICATIONS";
            }
            {
              enabled = true;
              name = "SYSTEM_PREFS";
            }
            {
              enabled = true;
              name = "MENU_EXPRESSION";
            } # Calculator
            {
              enabled = true;
              name = "MENU_CONVERSION";
            } # Unit conversion
            {
              enabled = true;
              name = "MENU_DEFINITION";
            } # Dictionary
            {
              enabled = false;
              name = "DIRECTORIES";
            } # Folders
            {
              enabled = false;
              name = "PDF";
            }
            {
              enabled = false;
              name = "DOCUMENTS";
            }
            {
              enabled = false;
              name = "FONTS";
            }
            {
              enabled = false;
              name = "MESSAGES";
            }
            {
              enabled = false;
              name = "CONTACT";
            }
            {
              enabled = false;
              name = "EVENT_TODO";
            }
            {
              enabled = false;
              name = "IMAGES";
            }
            {
              enabled = false;
              name = "BOOKMARKS";
            }
            {
              enabled = false;
              name = "MUSIC";
            }
            {
              enabled = false;
              name = "MOVIES";
            }
            {
              enabled = false;
              name = "PRESENTATIONS";
            }
            {
              enabled = false;
              name = "SPREADSHEETS";
            }
            {
              enabled = false;
              name = "SOURCE";
            }
            {
              enabled = false;
              name = "MENU_OTHER";
            }
            {
              enabled = false;
              name = "MENU_WEBSEARCH";
            } # Siri suggestions
            {
              enabled = false;
              name = "MENU_SPOTLIGHT_SUGGESTIONS";
            }
          ];
        };
      };
    };

    # keyboard = {
    #   enableKeyMapping = true;
    #   remapCapsLockToControl = true;
    # };
  };

  # TODO: module users
  users.users."drewry.pope" = {
    name = "drewry.pope";
    home = "/Users/drewry.pope";
  };
  # TODO: module home-manager
  home-manager = {

    useGlobalPkgs = true;
    useUserPackages = true;
    users = {
      # TODO: module per-user home manager
      "drewry.pope" = import ./homeUser.nix;
    };
    sharedModules = [
      inputs.mac-app-util.homeManagerModules.default
    ];
    extraSpecialArgs = {
      inherit inputs system;
    };
    backupFileExtension = ".backup";
  };
  # TODO: module nix-homebrew
  nix-homebrew = {
    enable = true;
    enableRosetta = true;
    user = "drewry.pope";
    # Workaround: nix-homebrew uses Ruby 4.0 but brew vendors gems under ruby/3.4.0.
    # bundler/setup.rb resolves to ruby/4.0.0/ which doesn't exist, so gems like
    # sorbet-runtime fail to load. Also, install_bundler_gems! tries to write into the
    # read-only Nix store (gems.rb mkpath). Fix both by:
    # 1. Symlinking ruby/4.0.0 → 3.4.0 so bundler/setup.rb finds vendored gems
    # 2. Setting HOMEBREW_SKIP_INITIAL_GEM_INSTALL to prevent writes to Nix store
    # ref: https://github.com/zhaofengli/nix-homebrew/issues/35
    # Ruby 4.0 symlink workaround removed — brew 5.1.11 ships with ruby/4.0.0 natively
    package = inputs.brew-src;
    extraEnv.HOMEBREW_SKIP_INITIAL_GEM_INSTALL = "1";
    taps = {
      "homebrew/homebrew-core" = inputs.homebrew-core;
      "homebrew/homebrew-cask" = inputs.homebrew-cask;
      "homebrew/homebrew-bundle" = inputs.homebrew-bundle;
      # "homebrew/homebrew-services" = inputs.nixpkgs.legacyPackages."${pkgs.system}".applyPatches
      # {
      #   name = "homebrew-services-patched"; # https://github.com/zhaofengli/nix-homebrew/issues/13#issuecomment-2156223912
      #   src = inputs.homebrew-services;
      #   patches = [ ./homebrew-services.patch ];
      # };
      "null-dev/homebrew-firefox-profile-switcher" = inputs.homebrew-firefox-profile-switcher;
      "Dimentium/homebrew-autoraise" = inputs.homebrew-autoraise;
      "gromgit/homebrew-fuse" = inputs.homebrew-fuse;
      "gabrimatic/homebrew-local-whisper" = inputs.homebrew-local-whisper;

    };
    mutableTaps = false;
    autoMigrate = true;
    # brew 6.x requires explicit trust for non-official taps (HOMEBREW_REQUIRE_TAP_TRUST).
    # nix-homebrew runs `brew trust` for these during activation so it is declarative and
    # survives fresh setups. Prefer specific formulae over whole-tap trust.
    # ref: https://github.com/zhaofengli/nix-homebrew README "trust" + https://docs.brew.sh/Tap-Trust
    trust = {
      formulae = [
        "null-dev/firefox-profile-switcher/firefox-profile-switcher-connector"
        "dimentium/autoraise/autoraise"
        "gromgit/fuse/bindfs-mac"
        "gabrimatic/local-whisper/local-whisper"
      ];
    };
  };
  # TODO: module homebrew
  homebrew = {
    # https://github.com/BatteredBunny/brew-nix
    # https://github.com/jcszymansk/nixcasks
    enable = true;
    global = {
      # lockfiles
      # brewFile
      autoUpdate = true;
    };
    # brewOptions
    # caskArgsOptions
    # tapOptions
    onActivation = {
      autoUpdate = true;
      # TODO: try to fix. "uninstall" would auto-remove casks dropped from
      # casks.nix (exactly the drift this leaves behind: renamed casks such as
      # handbrake -> handbrake-app leave BOTH installed), but nix-homebrew
      # currently emits an obsolete --force-cleanup flag with it. Until then,
      # removed casks are NOT uninstalled and ./clean only reports the drift.
      cleanup = "none"; # "uninstall" generates obsolete --force-cleanup flag
      upgrade = true;
      extraFlags = [
        "--verbose"
        "--cleanup"
        "--force"
      ];
    };
    # caskArgs
    taps = builtins.attrNames config.nix-homebrew.taps;
    # brewfile
    # extraConfig
    # whalebrews
    # preInstalledAndNotFoundInNixOrBrewOrAppStore = [ # apps that are pre-installed on a macOS but not found in nixpkgs, homebrew, or the Mac App Store
    #   "arctic-wolf-agent-manager"
    #   "arctic-wolf-agent-notifier"
    #   "company-portal"
    #   "jamf-connect"
    #   "thousandeyes-endpoint-agent"
    #   "vertex-inc--self-service"
    #   "workday"
    # ];
  };
  nix = {
    enable = false;
    # package = pkgs.nixVersions.nix_2_24;
    package = lib.mkForce pkgs.nixVersions.git; # forcing because determinate nix wants an older version, if problems try commenting the above line and reverting to the determinate nix version, probably 2.24.10 or something
    # package = pkgs.nixVersions.nix_2_25;
    # package = pkgs.nixVersions.nix_2_26;
    # package = pkgs.nixVersions.nix_2_42;
  };
  #   configureBuildUsers = true;
  #   extraOptions = ''
  #     extra-nix-path = nixpkgs=flake:nixpkgs
  #     upgrade-nix-store-path-url = https://install.determinate.systems/nix-upgrade/stable/universal
  #   '';
  #   gc = {
  #     user = "root";
  #     automatic = true;
  #     interval = { Weekday = 0; Hour = 2; Minute = 0; };
  #     options = "--delete-older-than 30d";
  #   };
  # };
  # system.checks.verifyNixPath = false;
  system.primaryUser = "drewry.pope";

  # Restart skhd after rebuild so config changes take effect
  # Fix Spotlight indexing and exclude noisy directories
  # Clean up stale Caskroom artifacts before homebrew runs (e.g. after cask renames)
  system.activationScripts.preActivation.text = ''
    stale_casks="mkvtoolnix-app"
    for stale in $stale_casks; do
      if [ -e "/opt/homebrew/Caskroom/$stale" ]; then
        echo "Removing stale Caskroom: $stale"
        rm -rf "/opt/homebrew/Caskroom/$stale"
      fi
    done
  '';

  # SidePulse's lid-closed LED feature needs passwordless `pmset -a disablesleep`.
  # Upstream ships this as a GUI prompt that opens Terminal and asks for a sudo
  # password (`sidepulse status-bar install-sleep-helper`). Installing the same
  # rule here makes it reproducible and removes the interactive step.
  #
  # Deliberately NOT environment.etc: that symlinks through /etc/static, and
  # nix-darwin refuses to take over an /etc path that already exists as a real
  # file -- which this one does, because the GUI installer already wrote it.
  # Writing it directly also lets us match upstream's bytes exactly and validate
  # with visudo before moving it into place.
  #
  # Must stay byte-identical to sleep_helper_sudoers_rule() in
  # src/sidepulse/lid_sleep.py, or sidepulse decides the file drifted and
  # re-prompts on every launch.
  system.activationScripts.extraActivation.text = ''
    sidepulseSudoers=/etc/sudoers.d/sidepulse-disablesleep
    sidepulseRule='${config.system.primaryUser} ALL=(root) NOPASSWD: /usr/bin/pmset -a disablesleep 0, /usr/bin/pmset -a disablesleep 1'

    if [ "$(cat "$sidepulseSudoers" 2>/dev/null)" != "$sidepulseRule" ]; then
      echo "installing $sidepulseSudoers" >&2
      sidepulseTmp="$(/usr/bin/mktemp /etc/sudoers.d/.sidepulse-disablesleep.XXXXXX)"
      printf '%s\n' "$sidepulseRule" > "$sidepulseTmp"
      /usr/sbin/chown root:wheel "$sidepulseTmp"
      /bin/chmod 0440 "$sidepulseTmp"
      # Never install a sudoers file that does not parse -- a bad one can lock
      # sudo out entirely.
      if /usr/sbin/visudo -cf "$sidepulseTmp" >/dev/null; then
        /bin/mv "$sidepulseTmp" "$sidepulseSudoers"
      else
        /bin/rm -f "$sidepulseTmp"
        echo "sidepulse sudoers rule failed visudo validation; not installed" >&2
      fi
    fi
  '';

  system.activationScripts.postActivation.text = ''
    # =====================================================================
    # Menu bar / notch space — auto-hide + tighter icon spacing
    # =====================================================================
    # Hide menu bar in fullscreen (apps use full screen beside notch)
    /usr/bin/defaults write -g AppleMenuBarVisibleInFullscreen -bool false

    # Reduce menu bar icon spacing so more fits beside the notch
    /usr/bin/defaults -currentHost write -globalDomain NSStatusItemSpacing -int 6
    /usr/bin/defaults -currentHost write -globalDomain NSStatusItemSelectionPadding -int 6

    # Disable font smoothing (subpixel antialiasing) for sharper text on retina
    /usr/bin/defaults -currentHost write -g AppleFontSmoothing -int 0

    if /bin/launchctl list | grep -q org.nixos.skhd; then
      /bin/launchctl kickstart -k "gui/$(id -u)/org.nixos.skhd" || true
    fi

    # Root's git: /etc/gitconfig already applies at system scope, but root has no
    # global config of its own, so `sudo git` and clean.sh's root git calls miss
    # anything a future change puts at global scope only. Pin root's global scope
    # to the same generated file.
    ln -sfn /etc/gitconfig /var/root/.gitconfig


    # =====================================================================
    # Default browser
    # =====================================================================
    # macOS has no nix-darwin option for this: the default browser is a
    # LaunchServices URL-scheme handler, stored PER USER in
    # ~/Library/Preferences/com.apple.LaunchServices/com.apple.launchservices.secure.plist
    # Activation runs as root, so duti must be run as the primary user or it
    # would set root's handlers instead.
    #
    # Declaring it here means an app reinstall (or a cask being removed and
    # restored) can never silently leave the default pointing somewhere else.
    # Idempotent: duti rewrites the same value and exits 0 when already set.
    # Exception: re-asserting an ALREADY-correct https handler can return
    # error -54 (LaunchServices refusing a redundant write). That is harmless
    # and the handler stays correct, hence `|| true` rather than a hard fail.
    #
    # NOTE: LaunchServices keeps the handler keyed by bundle id even while the
    # app is absent, so this survives uninstall/reinstall cycles by itself --
    # this just guarantees it.
    #
    # Zen stable and Zen Twilight share bundle id app.zen-browser.zen, so the
    # handler cannot distinguish them; macOS resolves it to whichever bundle it
    # last registered. Prefer Twilight by re-registering it with lsregister so
    # it wins, then assert the handler.
    zen_app=""
    for candidate in /Applications/Twilight.app /Applications/Zen.app; do
      [ -d "$candidate" ] && { zen_app="$candidate"; break; }
    done
    if [ -n "$zen_app" ]; then
      primary_uid_browser=$(/usr/bin/id -u ${config.system.primaryUser})
      /System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister \
        -f "$zen_app" 2>/dev/null || true
      for scheme in http https; do
        /usr/bin/sudo -u ${config.system.primaryUser} \
          /usr/bin/launchctl asuser "$primary_uid_browser" \
          ${pkgs.duti}/bin/duti -s app.zen-browser.zen "$scheme" 2>/dev/null || true
      done
      # HTML documents opened from Finder
      /usr/bin/sudo -u ${config.system.primaryUser} \
        /usr/bin/launchctl asuser "$primary_uid_browser" \
        ${pkgs.duti}/bin/duti -s app.zen-browser.zen public.html all 2>/dev/null || true
    fi

    # Run the guarded Colima startup check after every switch.
    primary_uid=$(/usr/bin/id -u ${config.system.primaryUser})
    /bin/launchctl asuser "$primary_uid" \
      /usr/bin/sudo -u ${config.system.primaryUser} \
      /bin/launchctl kickstart "gui/$primary_uid/org.nixos.colima" || true

    # =====================================================================
    # Spotlight indexing fixes
    # =====================================================================
    # On Apple Silicon, `/` is the sealed, read-only Signed System Volume.
    # Every writable user/data path lives on /System/Volumes/Data, so both the
    # health check and the rebuild must target the data volume — running them
    # against `/` is effectively a no-op.
    spotlight_volume="/System/Volumes/Data"

    # Disable Spotlight indexing on /nix (huge read-only store, never useful).
    # /nix is its own APFS volume, so the marker + mdutil cover the whole store,
    # including the nix cache under /nix/var.
    if [ -d /nix ]; then
      /usr/bin/mdutil -i off /nix 2>/dev/null || true
      # Marker file tells Spotlight to never index this volume/directory
      /usr/bin/touch /nix/.metadata_never_index 2>/dev/null || true
    fi

    # If Spotlight is stuck in transitioning state, rebuild the index.
    # Failures are reported rather than silently swallowed — a hidden failure
    # here is indistinguishable from a healthy index.
    if /usr/bin/mdutil -s "$spotlight_volume" 2>&1 | grep -q "kMDConfigSearchLevelTransitioning"; then
      echo "Spotlight stuck in transitioning state — rebuilding index on $spotlight_volume..."
      if ! /usr/bin/mdutil -E "$spotlight_volume"; then
        echo "warning: Spotlight reindex failed on $spotlight_volume" >&2
      fi
    fi

    # Spotlight never-index markers + git system setup.
    #
    # Both helpers are referenced from the flake source, so they land in the nix
    # store and activation never depends on the working copy's path. Each is run
    # twice, because both are $HOME-relative and neither account can see the
    # other's paths:
    #   as root  -> /Library, /nix, /var/root/*  (and root's git config)
    #   as user  -> $HOME/*                      (and the launchd scheduler)
    #
    # The markers are created with plain `touch`, i.e. REAL root-owned files
    # rather than read-only store symlinks, so anything that clears caches (./clean
    # wipes ~/Library/Caches outright) can delete them. Re-asserting them on every
    # activation is the point.
    spotlight_exclude=${./spotlight-exclude-artifacts}
    git_maintain=${./git-maintain-repos}
    as_user="/bin/launchctl asuser $primary_uid /usr/bin/sudo -u ${config.system.primaryUser}"
    helper_env="/usr/bin/env PATH=/run/current-system/sw/bin:/usr/bin:/bin"

    # exclude: --system re-asserts the static list only (~40 ms). Consider
    # dropping --system to get the default --auto instead: it adds the per-repo
    # walk for only ~3 s, and would mark newly-created repos automatically.
    $helper_env /run/current-system/sw/bin/bash "$spotlight_exclude" --system || true
    # maintain: --system only (~80 ms). Do NOT drop --system here -- the default
    # scope is --auto, which walks every repo under ~/git and takes roughly 3
    # minutes. Adding --deep on top would also expire reflogs and prune. Run
    # `just git-maintain` / `just git-maintain-quick` deliberately instead.
    $helper_env /run/current-system/sw/bin/bash "$git_maintain" --system || true

    # shellcheck disable=SC2086
    # same trade-off as above: --system ~40 ms, default --auto adds the ~3 s walk
    $as_user $helper_env /run/current-system/sw/bin/bash "$spotlight_exclude" --system || true
    # shellcheck disable=SC2086
    # --system only. No scheduler is installed for root: macOS offers only
    # launchd, and `git maintenance start` writes launchd *agents*, which
    # require a GUI Aqua session that root does not have.
    $as_user $helper_env /run/current-system/sw/bin/bash "$git_maintain" --system || true

    # Point xcode-select at full Xcode.app if installed (idempotent, instant)
    if [ -d "/Applications/Xcode.app/Contents/Developer" ]; then
      /usr/bin/xcode-select -s /Applications/Xcode.app/Contents/Developer 2>/dev/null || true
      # Accept Xcode license (no-op if already accepted)
      /usr/bin/xcodebuild -license accept 2>/dev/null || true
      # Install additional components on first launch (no-op if already done)
      /usr/bin/xcodebuild -runFirstLaunch 2>/dev/null || true
    fi
  '';

  # Root-scoped git maintenance. `git maintenance start` only knows how to write
  # launchd *Agents*, which need an Aqua session that root does not have, so git
  # itself cannot schedule anything for root. A LaunchDaemon runs headless and
  # can, invoking exactly what the user's agent does.
  #
  # This iterates ROOT's maintenance.repo list, which is empty unless root-owned
  # repositories are registered, so today it is a no-op placeholder. The primary
  # user's 60-odd repos are covered by their own launchd agent instead.
  launchd.daemons.git-maintenance = {
    serviceConfig = {
      ProgramArguments = [
        "/run/current-system/sw/bin/git"
        "for-each-repo"
        "--keep-going"
        "--config=maintenance.repo"
        "maintenance"
        "run"
        "--schedule=daily"
      ];
      StartCalendarInterval = [
        {
          Hour = 3;
          Minute = 30;
        }
      ];
      RunAtLoad = false;
      StandardErrorPath = "/var/log/git-maintenance.err.log";
      StandardOutPath = "/var/log/git-maintenance.out.log";
    };
  };

  # TODO: module launchd
  launchd.user.agents = {
    colima = {
      path = [ pkgs.colima ];
      script = ''
        if ! colima status >/dev/null 2>&1; then
          colima start
        fi
      '';
      serviceConfig = {
        EnvironmentVariables.HOME = "/Users/drewry.pope";
        KeepAlive = false;
        RunAtLoad = true;
        StandardErrorPath = "/tmp/colima.err.log";
        StandardOutPath = "/tmp/colima.out.log";
      };
    };
    naturalScrollingToggle = {
      path = [ config.environment.systemPath ];
      serviceConfig = {
        KeepAlive = false;
        RunAtLoad = true;
        ProgramArguments = [
          "/bin/sh"
          "-c"
          "if system_profiler SPUSBDataType | grep -i \"Mouse\"; then defaults write NSGlobalDomain com.apple.swipescrolldirection -bool false; else defaults write NSGlobalDomain com.apple.swipescrolldirection -bool true; fi && killall Finder"
        ];
        StandardErrorPath = "/tmp/natural_scrolling.err.log";
        StandardOutPath = "/tmp/natural_scrolling.out.log";
      };
    };
    lootbox = {
      path = [
        pkgs.deno
        pkgs.nodejs
        inputs.self.packages.${system}.codedb
        pkgs.codebase-memory-mcp
        pkgs.fff-mcp
      ];
      serviceConfig = {
        EnvironmentVariables.HOME = "/Users/drewry.pope";
        EnvironmentVariables.LOOTBOX_UI_DIR = "/Users/drewry.pope/.local/share/lootbox/ui";
        EnvironmentVariables.PATH = lib.mkForce (
          lib.makeBinPath [
            pkgs.bash
            pkgs.deno
            pkgs.nodejs
            inputs.self.packages.${system}.codedb
            pkgs.codebase-memory-mcp
            pkgs.fff-mcp
          ]
          + ":/Users/drewry.pope/.local/share/lootbox/npm/node_modules/.bin"
        );
        KeepAlive = true;
        RunAtLoad = true;
        WorkingDirectory = "/Users/drewry.pope/.config/nix";
        ProgramArguments = [
          "/Users/drewry.pope/.local/bin/lootbox"
          "server"
          "--port"
          "9420"
          "--lootbox-root"
          "/Users/drewry.pope/.config/nix/.lootbox"
        ];
        StandardErrorPath = "/tmp/lootbox.err.log";
        StandardOutPath = "/tmp/lootbox.out.log";
      };
    };
  };
} (import ./nix.settings.nix) # TODO: module nix-settings
