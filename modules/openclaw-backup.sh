# openclaw-backup — daily backup of the OpenClaw state dir (~/.openclaw)
# to three destinations (Drew, 2026-10-28; follows the pm-dashboard-backup
# pattern from code#1516 / nix#976).
#
# A dated tar.zst with a sha256 manifest in ~/backups/openclaw/<date>/,
# verified against its own manifest, then copied to Cloudflare R2
# (supabase-backups bucket, openclaw/<date>/) and Google Drive
# (Cloud/cold-store/db-backups/openclaw/<date>/), each copy sha256-
# verified against the local archive. 30-day retention everywhere;
# pruning only ever removes this job's own dated dirs and never the
# current run's date.
#
# Data set: all of ~/.openclaw EXCEPT the scratch trees tmp/ (2.9 GB of
# session scratch) and cache/ (regenerable), the .git dir of the
# openclaw-personal checkout, and transient sqlite sidecars/locks. The
# agent state DB (state/openclaw.sqlite) is snapshotted with sqlite3
# .backup so the archived copy is transaction-consistent even while
# the gateway is running. Also included: the last-24h (or since-last-
# success) journal of the openclaw-gateway user service.
#
# Credentials come from Bitwarden Secrets Manager at runtime, referenced
# by key name only and never logged (the pm-dashboard-backup pattern):
# cloudflare.r2-backups-access-key-id, cloudflare.r2-backups-secret-
# access-key, cloudflare.r2-endpoint. The Drive leg uses the laptop's
# existing rclone "drive" remote. Declared as a systemd user
# service+timer in dezren39/nix (modules/laptop-backups.nix).

DATE="${1:-$(date +%F)}"
CREATED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
OC="$HOME/.openclaw"
ROOT="$HOME/backups/openclaw"
OUTDIR="$ROOT/$DATE"
ARCHIVE="$OUTDIR/openclaw-$DATE.tar.zst"
MARKER="$ROOT/.last-run-epoch"
R2_BUCKET="supabase-backups"
R2_PREFIX="openclaw"
DRIVE_DIR="Cloud/cold-store/db-backups/openclaw"

log() { echo "openclaw-backup: $*"; }
fail() { echo "openclaw-backup: FAILED $*" >&2; exit 1; }
STAGE=""
on_exit() {
  rc=$?
  if [ -n "$STAGE" ]; then rm -rf "$STAGE"; fi
  if [ $rc -ne 0 ]; then echo "openclaw-backup: FAILED exit=$rc" >&2; fi
}
trap on_exit EXIT

# --- DATE validation (same discipline as pm-dashboard-backup #989) ---
if [[ ! "$DATE" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ ]] \
  || [ "$(date -d "$DATE" +%F 2>/dev/null)" != "$DATE" ]; then
  fail "invalid DATE '$DATE' - expected a real calendar date as YYYY-MM-DD"
fi

[ -d "$OC" ] || fail "no OpenClaw state dir at $OC"
mkdir -p "$ROOT"

# --- single instance -------------------------------------------------------
exec 9>"$ROOT/.openclaw-backup.lock"
if ! flock -n 9; then
  log "SKIP another openclaw-backup run holds the lock"
  exit 0
fi

# --- tools (same resolutions as pm-dashboard-backup) -----------------------
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
command -v sqlite3 >/dev/null 2>&1 || fail "sqlite3 not in PATH"

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
export RCLONE_CONFIG_R2BAK_TYPE="s3"
export RCLONE_CONFIG_R2BAK_PROVIDER="Cloudflare"
export RCLONE_CONFIG_R2BAK_ACCESS_KEY_ID="$R2_ACCESS_KEY_ID"
export RCLONE_CONFIG_R2BAK_SECRET_ACCESS_KEY="$R2_SECRET_ACCESS_KEY"
export RCLONE_CONFIG_R2BAK_ENDPOINT="$R2_ENDPOINT"
export RCLONE_CONFIG_R2BAK_REGION="auto"
# The r2-backups token cannot HeadBucket (403), so rclone must skip its
# bucket check — otherwise it falls back to CreateBucket and fails with
# AccessDenied before any upload (found 2026-10-28; pm-dashboard-backup
# has the same latent bug).
export RCLONE_CONFIG_R2BAK_NO_CHECK_BUCKET="true"

# === leg 1: the local archive ==============================================
STAGE="$(mktemp -d)"
mkdir -p "$OUTDIR"

# --- data set: the state dir minus scratch/transients ---
tar -cf - -C "$OC" \
  --exclude='./tmp' --exclude='./cache' --exclude='./.git' \
  --exclude='*.sqlite-wal' --exclude='*.sqlite-shm' --exclude='*.lock' \
  . | tar -xf - -C "$STAGE" || fail "staging copy of $OC failed"

# --- transaction-consistent snapshot of the agent state DB on top ---
if [ -f "$OC/state/openclaw.sqlite" ]; then
  mkdir -p "$STAGE/state"
  sqlite3 "$OC/state/openclaw.sqlite" ".backup '$STAGE/state/openclaw.sqlite'" \
    || fail "sqlite .backup of state/openclaw.sqlite failed"
fi

# --- journal: window start = min(last successful run, 24h ago) ---
NOW_EPOCH="$(date +%s)"
LAST=0
if [ -f "$MARKER" ]; then LAST="$(cat "$MARKER" 2>/dev/null || echo 0)"; fi
case "$LAST" in (*[!0-9]*|"") LAST=0;; esac
W=$(( NOW_EPOCH - 86400 ))
if [ "$LAST" -gt 0 ] && [ "$LAST" -lt "$W" ]; then W="$LAST"; fi
SINCE_STR="$(date -d "@$W" '+%F %T')"
mkdir -p "$STAGE/journals"
journalctl --user -u "openclaw-gateway.service" --since "$SINCE_STR" --no-pager -o short-iso \
  > "$STAGE/journals/openclaw-gateway.log" 2>/dev/null \
  || echo "(journal unavailable for openclaw-gateway)" > "$STAGE/journals/openclaw-gateway.log"

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
    json.dump({"archive": f"openclaw-{date}", "created_at": created,
               "host": os.uname().nodename, "scope": "laptop",
               "files": files}, fh, indent=2)
print(f"openclaw-backup: manifest covers {len(files)} files")
PYEOF

# --- tarball (atomic replace) + verify against manifest ---
TMPARC="$OUTDIR/.openclaw-$DATE.tar.zst.tmp"
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
  echo "openclaw-backup: VERIFY FAILED for $ARCHIVE" >&2
  rm -rf "$VDIR"; exit 1
fi
rm -rf "$VDIR"

ARC_SHA="$(sha256sum "$ARCHIVE" | cut -d' ' -f1)"
log "local OK $ARCHIVE ($(stat -c%s "$ARCHIVE") bytes, sha256 $ARC_SHA)"

# --- prune local day dirs older than 30 days (never $DATE itself) ---
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

R2_DEST="r2bak:$R2_BUCKET/$R2_PREFIX/$DATE/openclaw-$DATE.tar.zst"
rclone copyto "$ARCHIVE" "$R2_DEST" || fail "R2 upload failed"
verify_remote "$R2_DEST" "R2"
prune_remote "r2bak:$R2_BUCKET/$R2_PREFIX" "R2"

DRIVE_DEST="drive:$DRIVE_DIR/$DATE/openclaw-$DATE.tar.zst"
rclone copyto "$ARCHIVE" "$DRIVE_DEST" || fail "Drive upload failed"
verify_remote "$DRIVE_DEST" "Drive"
prune_remote "drive:$DRIVE_DIR" "Drive"

# --- journal cursor: advance ONLY now that all three legs succeeded ---
echo "$NOW_EPOCH" > "$MARKER"

log "OK openclaw $DATE backed up to local + R2 + Drive (sha256 $ARC_SHA)"
