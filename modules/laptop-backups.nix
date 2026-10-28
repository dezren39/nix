# laptop-backups — the laptop's backup system of record, declared in the
# flake (Drew, 2026-10-27; developing-today/code#1516, dezren39/nix#976).
#
# Home-manager module: import it from the host config that manages the
# laptop user's services (the NixOS config in developing-today/code does)
# and set `services.laptop-backups.enable = true`.
#
# Jobs:
# - pm-dashboard-backup (daily ~04:30): the PM dashboard / orchestration
#   state set as a dated tar.zst with a sha256 manifest to the local
#   backup dir, Cloudflare R2 (supabase-backups) and Google Drive
#   (Cloud/cold-store/db-backups/pm-aggregator/<date>/), every copy
#   sha256-verified, 30-day retention. Runs the code#1514 DB-dump script
#   first when it exists at ~/tools/pm-stack-db-backup.sh, and its dumps
#   ride the same archive. Script: ./pm-dashboard-backup.sh (vendored
#   here; credentials from Bitwarden by name at runtime, never in the
#   store).
# - repo-archive-backup (daily ~05:15): the pre-existing restic job
#   (~/repo-archive -> local + R2 + Drive). The unit + timer are declared
#   here so the schedule lives in the flake; the script itself stays at
#   ~/tools/repo-archive-backup.sh, the path its author maintains.
# - openclaw-backup (daily ~04:05): the OpenClaw state dir (~/.openclaw,
#   minus the tmp/ and cache/ scratch trees) as a dated tar.zst with a
#   sha256 manifest to the local backup dir, R2 (supabase-backups,
#   openclaw/<date>/) and Google Drive (Cloud/cold-store/db-backups/
#   openclaw/<date>/), every copy sha256-verified, 30-day retention.
#   The agent state DB is snapshotted with sqlite3 .backup so the
#   archived copy is transaction-consistent. Script:
#   ./openclaw-backup.sh (vendored here; same credential pattern).
# - supabase-backup (twice daily 04:42 + 16:42): pg_dump of the muse-dev
#   Supabase project to R2 (supabase-backups), content-hashed names.
#   Migrated from the sandbox 2026-10-28 (developing-today/code#1723);
#   the unit is declared here per Drew's all-services-in-nix rule the
#   same day. Script: ~/tools/supabase-backup.sh (wrapper; the dump
#   program + config live in ~/tools/supabase-backup/).
# - cold-store-nightly (daily 03:12): the cold-store reaper's nightly
#   backup set (D1 exports, Supabase filing, Turso exports) to Google
#   Drive. Migrated from the sandbox 2026-10-28 (code#1723). Script:
#   ~/tools/cold-store-nightly.sh (wrapper; the job script itself lives
#   in developing-today/code, pkgs/cold-store-reaper).
# - cold-store-drill (monthly, day 1, 05:12; the script self-gates to
#   Jan/Apr/Jul/Oct): restore drill for the cold-store backups.
#   Script: ~/tools/cold-store-drill.sh (code#1723).
#
# Pruning discipline: each job prunes only the dated directories it
# created itself. The sandbox's backup job writes into its own tree under
# ~/backups on this host; nothing here rotates files it did not create.
{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.services.laptop-backups;
  home = config.home.homeDirectory;
  dashboardPkg = pkgs.writeShellScriptBin "pm-dashboard-backup" (
    builtins.readFile ./pm-dashboard-backup.sh
  );
  openclawPkg = pkgs.writeShellScriptBin "openclaw-backup" (
    builtins.readFile ./openclaw-backup.sh
  );
  servicePath = lib.makeBinPath [
    pkgs.coreutils
    pkgs.findutils
    pkgs.gnutar
    pkgs.zstd
    pkgs.python3
    pkgs.rclone
    pkgs.util-linux
    pkgs.systemd
  ];
in
{
  options.services.laptop-backups = {
    enable = lib.mkEnableOption "the laptop backup jobs (dashboard, repo archive, openclaw, supabase, cold-store)";

    dashboard = {
      enable = lib.mkOption {
        type = lib.types.bool;
        default = true;
        description = "Daily PM dashboard backup to local dir + R2 + Google Drive (code#1516).";
      };
      onCalendar = lib.mkOption {
        type = lib.types.str;
        default = "04:30";
        description = "Daily run time (local) for the dashboard backup.";
      };
    };

    repoArchive = {
      enable = lib.mkOption {
        type = lib.types.bool;
        default = true;
        description = "Daily restic repo-archive backup (pre-existing script, unit declared here).";
      };
      onCalendar = lib.mkOption {
        type = lib.types.str;
        default = "05:15";
        description = "Daily run time (local) for the repo-archive backup.";
      };
      script = lib.mkOption {
        type = lib.types.str;
        default = "${home}/tools/repo-archive-backup.sh";
        description = "Path of the repo-archive backup script to run.";
      };
    };

    openclaw = {
      enable = lib.mkOption {
        type = lib.types.bool;
        default = true;
        description = "Daily OpenClaw state backup (~/.openclaw) to local dir + R2 + Google Drive.";
      };
      onCalendar = lib.mkOption {
        type = lib.types.str;
        default = "04:05";
        description = "Daily run time (local) for the OpenClaw backup.";
      };
    };

    supabase = {
      enable = lib.mkOption {
        type = lib.types.bool;
        default = true;
        description = "Twice-daily pg_dump backup of the muse-dev Supabase project to R2 (code#1723).";
      };
      onCalendar = lib.mkOption {
        type = lib.types.listOf lib.types.str;
        default = [
          "04:42"
          "16:42"
        ];
        description = "Daily run times (local) for the Supabase backup.";
      };
      script = lib.mkOption {
        type = lib.types.str;
        default = "${home}/tools/supabase-backup.sh";
        description = "Path of the Supabase backup wrapper script to run.";
      };
    };

    coldStoreNightly = {
      enable = lib.mkOption {
        type = lib.types.bool;
        default = true;
        description = "Daily cold-store nightly backup set to Google Drive (code#1723).";
      };
      onCalendar = lib.mkOption {
        type = lib.types.str;
        default = "03:12";
        description = "Daily run time (local) for the cold-store nightly backup.";
      };
      script = lib.mkOption {
        type = lib.types.str;
        default = "${home}/tools/cold-store-nightly.sh";
        description = "Path of the cold-store nightly wrapper script to run.";
      };
    };

    coldStoreDrill = {
      enable = lib.mkOption {
        type = lib.types.bool;
        default = true;
        description = "Monthly cold-store restore drill; the script self-gates to Jan/Apr/Jul/Oct (code#1723).";
      };
      onCalendar = lib.mkOption {
        type = lib.types.str;
        default = "*-*-01 05:12:00";
        description = "Monthly run time (local, day 1) for the cold-store restore drill.";
      };
      script = lib.mkOption {
        type = lib.types.str;
        default = "${home}/tools/cold-store-drill.sh";
        description = "Path of the cold-store drill wrapper script to run.";
      };
    };
  };

  config = lib.mkIf cfg.enable {
    systemd.user.services = lib.mkMerge [
      (lib.mkIf cfg.dashboard.enable {
        pm-dashboard-backup = {
          Unit.Description = "Daily PM dashboard backup - local + R2 + Drive (code#1516, nix#976)";
          Service = {
            Type = "oneshot";
            ExecStart = "${dashboardPkg}/bin/pm-dashboard-backup";
            Environment = "PATH=${servicePath}:${home}/tools/bin:/run/current-system/sw/bin";
          };
        };
      })
      (lib.mkIf cfg.repoArchive.enable {
        repo-archive-backup = {
          Unit.Description = "Daily repo-archive backup (restic: local + R2 + Google Drive)";
          Service = {
            Type = "oneshot";
            ExecStart = cfg.repoArchive.script;
          };
        };
      })
      (lib.mkIf cfg.openclaw.enable {
        openclaw-backup = {
          Unit.Description = "Daily OpenClaw state backup - local + R2 + Drive";
          Service = {
            Type = "oneshot";
            ExecStart = "${openclawPkg}/bin/openclaw-backup";
            Environment = "PATH=${servicePath}:${home}/tools/bin:/run/current-system/sw/bin";
          };
        };
      })
      (lib.mkIf cfg.supabase.enable {
        supabase-backup = {
          Unit.Description = "Twice-daily Supabase muse-dev pg_dump backup to R2 (code#1723)";
          Service = {
            Type = "oneshot";
            ExecStart = cfg.supabase.script;
            TimeoutStartSec = "30m";
          };
        };
      })
      (lib.mkIf cfg.coldStoreNightly.enable {
        cold-store-nightly = {
          Unit.Description = "Cold-store nightly backup set (D1 + Supabase + Turso exports to Drive) (code#1723)";
          Service = {
            Type = "oneshot";
            ExecStart = cfg.coldStoreNightly.script;
            TimeoutStartSec = "30m";
          };
        };
      })
      (lib.mkIf cfg.coldStoreDrill.enable {
        cold-store-drill = {
          Unit.Description = "Cold-store restore drill (quarterly months; script self-gates) (code#1723)";
          Service = {
            Type = "oneshot";
            ExecStart = cfg.coldStoreDrill.script;
            TimeoutStartSec = "15m";
          };
        };
      })
    ];

    systemd.user.timers = lib.mkMerge [
      (lib.mkIf cfg.dashboard.enable {
        pm-dashboard-backup = {
          Unit.Description = "Run the PM dashboard backup daily (code#1516)";
          Timer = {
            OnCalendar = cfg.dashboard.onCalendar;
            Persistent = true;
            RandomizedDelaySec = "15m";
          };
          Install.WantedBy = [ "timers.target" ];
        };
      })
      (lib.mkIf cfg.repoArchive.enable {
        repo-archive-backup = {
          Unit.Description = "Run the repo-archive backup daily";
          Timer = {
            OnCalendar = cfg.repoArchive.onCalendar;
            Persistent = true;
            RandomizedDelaySec = "15m";
          };
          Install.WantedBy = [ "timers.target" ];
        };
      })
      (lib.mkIf cfg.openclaw.enable {
        openclaw-backup = {
          Unit.Description = "Run the OpenClaw backup daily";
          Timer = {
            OnCalendar = cfg.openclaw.onCalendar;
            Persistent = true;
            RandomizedDelaySec = "15m";
          };
          Install.WantedBy = [ "timers.target" ];
        };
      })
      (lib.mkIf cfg.supabase.enable {
        supabase-backup = {
          Unit.Description = "Run the Supabase muse-dev backup twice daily";
          Timer = {
            OnCalendar = cfg.supabase.onCalendar;
            Persistent = true;
            RandomizedDelaySec = "15m";
          };
          Install.WantedBy = [ "timers.target" ];
        };
      })
      (lib.mkIf cfg.coldStoreNightly.enable {
        cold-store-nightly = {
          Unit.Description = "Run the cold-store nightly backup daily";
          Timer = {
            OnCalendar = cfg.coldStoreNightly.onCalendar;
            Persistent = true;
            RandomizedDelaySec = "15m";
          };
          Install.WantedBy = [ "timers.target" ];
        };
      })
      (lib.mkIf cfg.coldStoreDrill.enable {
        cold-store-drill = {
          Unit.Description = "Run the cold-store restore drill monthly (script self-gates to quarterly months)";
          Timer = {
            OnCalendar = cfg.coldStoreDrill.onCalendar;
            Persistent = true;
            RandomizedDelaySec = "15m";
          };
          Install.WantedBy = [ "timers.target" ];
        };
      })
    ];
  };
}
