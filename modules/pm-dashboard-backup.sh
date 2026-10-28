# pm-dashboard-backup — daily backup of the PM dashboard / orchestration
# data set to three destinations (Drew, 2026-10-27; code#1516, nix#976).
#
# Supersedes the hand-built pair from code#1459 (~/tools/dashboard-backup.sh
# on the laptop + the sandbox cron's offsite leg). The local leg is that
# script's archive build, unchanged: a dated tar.zst with a sha256 manifest
# in ~/backups/dashboard/<date>/, verified against its own manifest, 30-day
# retention. The offsite legs copy the same archive to Cloudflare R2
# (supabase-backups bucket, dashboard/<date>/) and to Google Drive
# (Cloud/cold-store/db-backups/pm-aggregator/<date>/), each verified by
# sha256 against the local archive, with the same 30-day retention.
#
# Retention never prunes the current run's own date (#982): a backfill
# or re-run for a date older than the cutoff must not delete the
# archive it just built and verified before (or just uploaded by) the
# offsite legs. That date is pruned by a later run, once it is no
# longer the run date.
#
# Credentials come from Bitwarden Secrets Manager at runtime, referenced
# by key name only and never logged (the repo-archive-backup pattern):
# cloudflare.r2-backups-access-key-id, cloudflare.r2-backups-secret-
# access-key, cloudflare.r2-endpoint. The Drive leg uses the laptop's
# existing rclone "drive" remote. Runs as a systemd user service+timer
# declared in dezren39/nix (modules/laptop-backups.nix).

DATE="${1:-$(date +%F)}"
CREATED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
AGG="$HOME/.local/state/pm-aggregator"
META="$HOME/atlas/meta-quota-export"
ROOT="$HOME/backups/dashboard"
OUTDIR="$ROOT/$DATE"
ARCHIVE="$OUTDIR/dashboard-$DATE.tar.zst"
MARKER="$ROOT/.last-run-epoch"
R2_BUCKET="supabase-backups"
R2_PREFIX="dashboard"
DRIVE_DIR="Cloud/cold-store/db-backups/pm-aggregator"

log() { echo "pm-dashboard-backup: $*"; }
fail() { echo "pm-dashboard-backup: FAILED $*" >&2; exit 1; }
trap 'rc=$?; if [ $rc -ne 0 ]; then echo "pm-dashboard-backup: FAILED exit=$rc" >&2; fi' EXIT

# --- DATE validation (#989) -------------------------------------------------
# DATE becomes a directory under $ROOT, the archive filename, and both
# rclone destination prefixes, and the retention prunes assume a real
# YYYY-MM-DD date (local glob, remote regex). Validate before any side
# effect: an unvalidated value could never be pruned ('not-a-date'),
# escaped the backup root ('../../x'), or named a day that does not
# exist ('2026-02-30'). The round-trip through date -d rejects
# normalized non-dates; the regex pins the strict shape.
if [[ ! "$DATE" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ ]] \
  || [ "$(date -d "$DATE" +%F 2>/dev/null)" != "$DATE" ]; then
  fail "invalid DATE '$DATE' - expected a real calendar date as YYYY-MM-DD"
fi

mkdir -p "$ROOT"

# --- single instance -------------------------------------------------------
exec 9>"$ROOT/.pm-dashboard-backup.lock"
if ! flock -n 9; then
  log "SKIP another pm-dashboard-backup run holds the lock"
  exit 0
fi

# --- tools -----------------------------------------------------------------
# rclone/bws may live outside the service PATH on this host (the
# repo-archive-backup resolutions, kept): rclone from the newest
# nix-store build, bws from ~/tools/bin.
if ! command -v rclone >/dev/null 2>&1; then
  RCLONE_BIN="$(ls -d /nix/store/*-rclone-[0-9]*/bin/rclone 2>/dev/null | sort -V | tail -1)"
  [ -n "$RCLONE_BIN" ] && [ -x "$RCLONE_BIN" ] || fail "rclone not found in PATH or nix store"
  mkdir -p "$ROOT/.bin"
  ln -sf "$RCLONE_BIN" "$ROOT/.bin/rclone"
  export PATH="$ROOT/.bin:$PATH"
fi
if ! command -v bws >/dev/null 2>&1 && [ -x "$HOME/tools/bin/bws" ]; then
  export PATH="$HOME/tools/bin:$PATH"
fi
command -v bws >/dev/null 2>&1 || fail "bws not in PATH"

# --- credentials from Bitwarden (names only in code/logs) ------------------
if [ -z "${BWS_ACCESS_TOKEN:-}" ] && [ -f "$HOME/.config/bws/access-token" ]; then
  BWS_ACCESS_TOKEN="$(cat "$HOME/.config/bws/access-token")"
  export BWS_ACCESS_TOKEN
fi
BWS_LIST="$(bws secret list 2>/dev/null)" || fail "bws secret list failed"
bws_get() {
  printf '%s' "$BWS_LIST" | python3 -c 'import json,sys
k = sys.argv[1]
for s in json.load(sys.stdin):
    if s.get("key") == k:
        print(s.get("value", "")); break' "$1"
}
R2_ACCESS_KEY_ID="$(bws_get cloudflare.r2-backups-access-key-id)"
R2_SECRET_ACCESS_KEY="$(bws_get cloudflare.r2-backups-secret-access-key)"
R2_ENDPOINT="$(bws_get cloudflare.r2-endpoint)"
[ -n "$R2_ACCESS_KEY_ID" ] && [ -n "$R2_SECRET_ACCESS_KEY" ] && [ -n "$R2_ENDPOINT" ] \
  || fail "R2 credentials incomplete in Bitwarden"
case "$R2_ENDPOINT" in
  http://*|https://*) ;;
  *) R2_ENDPOINT="https://$R2_ENDPOINT" ;;
esac
R2_ENDPOINT="${R2_ENDPOINT%/}"
# An env-configured rclone remote for R2 (no rclone.conf edit needed).
export RCLONE_CONFIG_R2BAK_TYPE="s3"
export RCLONE_CONFIG_R2BAK_PROVIDER="Cloudflare"
export RCLONE_CONFIG_R2BAK_ACCESS_KEY_ID="$R2_ACCESS_KEY_ID"
export RCLONE_CONFIG_R2BAK_SECRET_ACCESS_KEY="$R2_SECRET_ACCESS_KEY"
export RCLONE_CONFIG_R2BAK_ENDPOINT="$R2_ENDPOINT"
export RCLONE_CONFIG_R2BAK_REGION="auto"


# --- optional pm-stack DB dumps (code#1514) --------------------------------
# The #1514 dump job (Kaneo Postgres + Grist + Pad) publishes its script at
# ~/tools/pm-stack-db-backup.sh and writes dumps to ~/Backups/db-dumps/<date>/
# (here: ~/backups/db-dumps/<date>/). When present it runs first and its
# output rides this archive to all three destinations; until it lands, the
# archive carries the dashboard state set only and says so in the log.
DUMP_SCRIPT="${PM_DB_DUMP_SCRIPT:-$HOME/tools/pm-stack-db-backup.sh}"
DUMP_DIR="$HOME/backups/db-dumps/$DATE"
if [ -x "$DUMP_SCRIPT" ]; then
  log "running DB dump step: $DUMP_SCRIPT"
  "$DUMP_SCRIPT" "$DATE" || fail "DB dump step failed"
else
  log "no DB dump script at $DUMP_SCRIPT (code#1514 pending) - dashboard state only"
fi

# === leg 1: the local archive (dashboard-backup.sh, code#1459) ============
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
mkdir -p "$STAGE/aggregator" "$STAGE/meta-quota-export" "$STAGE/journals" "$OUTDIR"

# --- data set (explicit list only; the aggregator .env is deliberately NOT
# --- included — no config/secret files belong in this archive) ---
for f in actions.json comments.json fleet-feed.json quota-current.json \
         registry.json quota-gatherer-state.json quota-gatherer-heartbeat.json; do
  if [ -f "$AGG/$f" ]; then cp -a "$AGG/$f" "$STAGE/aggregator/$f";
  else echo "pm-dashboard-backup: WARN missing $AGG/$f" >&2; fi
done
if [ -d "$AGG/snapshots" ]; then cp -a "$AGG/snapshots" "$STAGE/aggregator/snapshots"; fi
for f in latest.json history.jsonl; do
  if [ -f "$META/$f" ]; then cp -a "$META/$f" "$STAGE/meta-quota-export/$f";
  else echo "pm-dashboard-backup: WARN missing $META/$f" >&2; fi
done

if [ -d "$DUMP_DIR" ]; then cp -a "$DUMP_DIR" "$STAGE/db-dumps"; fi

# --- journals: window start = min(last successful run, 24h ago) ---
NOW_EPOCH="$(date +%s)"
LAST=0
if [ -f "$MARKER" ]; then LAST="$(cat "$MARKER" 2>/dev/null || echo 0)"; fi
case "$LAST" in (*[!0-9]*|"") LAST=0;; esac
W=$(( NOW_EPOCH - 86400 ))
if [ "$LAST" -gt 0 ] && [ "$LAST" -lt "$W" ]; then W="$LAST"; fi
SINCE_STR="$(date -d "@$W" '+%F %T')"
for svc in pm-aggregator quota-gatherer meta-quota-export; do
  journalctl --user -u "$svc.service" --since "$SINCE_STR" --no-pager -o short-iso \
    > "$STAGE/journals/$svc.log" 2>/dev/null \
    || echo "(journal unavailable for $svc)" > "$STAGE/journals/$svc.log"
done

# --- manifest ---
python3 - "$STAGE" "$DATE" "$CREATED_AT" <<'PYEOF'
import hashlib, json, os, sys
stage, date, created = sys.argv[1], sys.argv[2], sys.argv[3]
files = []
for root, _dirs, names in os.walk(stage):
    for n in sorted(names):
        p = os.path.join(root, n)
        rel = os.path.relpath(p, stage)
        if rel == "manifest.json":
            continue
        h = hashlib.sha256()
        with open(p, "rb") as fh:
            for chunk in iter(lambda: fh.read(1 << 20), b""):
                h.update(chunk)
        files.append({"path": rel, "sha256": h.hexdigest(), "size": os.path.getsize(p)})
files.sort(key=lambda f: f["path"])
with open(os.path.join(stage, "manifest.json"), "w") as fh:
    json.dump({"archive": f"dashboard-{date}", "created_at": created,
               "host": os.uname().nodename, "scope": "laptop",
               "files": files}, fh, indent=2)
print(f"pm-dashboard-backup: manifest covers {len(files)} files")
PYEOF

# --- tarball (atomic replace) + verify against manifest ---
TMPARC="$OUTDIR/.dashboard-$DATE.tar.zst.tmp"
tar -I zstd -cf "$TMPARC" -C "$STAGE" .
mv "$TMPARC" "$ARCHIVE"
VDIR="$(mktemp -d)"
tar -I zstd -xf "$ARCHIVE" -C "$VDIR"
if ! ( cd "$VDIR" && python3 - <<'PYEOF'
import hashlib, json, os, sys
m = json.load(open("manifest.json"))
bad = 0
for f in m["files"]:
    p = f["path"]
    if not os.path.isfile(p):
        print("MISSING", p); bad += 1; continue
    if hashlib.sha256(open(p, "rb").read()).hexdigest() != f["sha256"] \
       or os.path.getsize(p) != f["size"]:
        print("MISMATCH", p); bad += 1
sys.exit(1 if bad else 0)
PYEOF
); then
  echo "pm-dashboard-backup: VERIFY FAILED for $ARCHIVE" >&2
  rm -rf "$VDIR"; exit 1
fi
rm -rf "$VDIR"

ARC_SHA="$(sha256sum "$ARCHIVE" | cut -d' ' -f1)"
log "local OK $ARCHIVE ($(stat -c%s "$ARCHIVE") bytes, sha256 $ARC_SHA)"

# --- prune local day dirs older than 30 days (only this script's own;
# --- never $DATE itself, #982 — this prune runs before the uploads) ---
CUTOFF="$(date -d '30 days ago' +%F)"
for d in "$ROOT"/20??-??-??; do
  [ -d "$d" ] || continue
  day="$(basename "$d")"
  if [[ "$day" != "$DATE" && "$day" < "$CUTOFF" ]]; then rm -rf "$d"; log "pruned local $day"; fi
done

# === legs 2+3: offsite copies, sha256-verified =============================
verify_remote() { # $1 rclone path, $2 label
  local got
  got="$(rclone cat "$1" 2>/dev/null | sha256sum | cut -d' ' -f1)"
  [ "$got" = "$ARC_SHA" ] || fail "$2 copy sha256 mismatch (got $got)"
  log "$2 OK sha256 verified ($1)"
}
prune_remote() { # $1 rclone dir holding <date> subdirs, $2 label
  local subs day
  subs="$(rclone lsf --dirs-only "$1" 2>/dev/null || true)"
  for day in $subs; do
    day="${day%/}"
    if [[ "$day" =~ ^20[0-9]{2}-[0-9]{2}-[0-9]{2}$ ]] && [[ "$day" != "$DATE" && "$day" < "$CUTOFF" ]]; then
      rclone purge "$1/$day" >/dev/null 2>&1 || fail "prune failed on $2 ($day)"
      log "pruned $2 $day"
    fi
  done
}

R2_DEST="r2bak:$R2_BUCKET/$R2_PREFIX/$DATE/dashboard-$DATE.tar.zst"
rclone copyto "$ARCHIVE" "$R2_DEST" || fail "R2 upload failed"
verify_remote "$R2_DEST" "R2"
prune_remote "r2bak:$R2_BUCKET/$R2_PREFIX" "R2"

DRIVE_DEST="drive:$DRIVE_DIR/$DATE/dashboard-$DATE.tar.zst"
rclone copyto "$ARCHIVE" "$DRIVE_DEST" || fail "Drive upload failed"
verify_remote "$DRIVE_DEST" "Drive"
prune_remote "drive:$DRIVE_DIR" "Drive"

# --- journal cursor: advance ONLY now that all three legs have succeeded ---
# (#981: this write used to sit right after the local leg, so a run that
# failed on R2/Drive still advanced the cursor and its journal window was
# never included in any later archive.)
echo "$NOW_EPOCH" > "$MARKER"

log "OK dashboard $DATE backed up to local + R2 + Drive (sha256 $ARC_SHA)"
