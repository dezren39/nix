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
# Every run also writes a durable JSON run record at
# ~/backups/dashboard/.last-run.json (#1026), read by the
# laptop-backups watchdog (modules/laptop-backups-watchdog.sh).
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
RECORD="$ROOT/.last-run.json"
R2_BUCKET="supabase-backups"
R2_PREFIX="dashboard"
DRIVE_DIR="Cloud/cold-store/db-backups/pm-aggregator"

log() { echo "pm-dashboard-backup: $*"; }
fail() { echo "pm-dashboard-backup: FAILED $*" >&2; exit 1; }
# One EXIT handler for both duties (#1010): bash traps replace, not
# stack, so a second `trap ... EXIT` for the staging dir (installed
# where STAGE is created below) silently discarded the FAILED-exit
# logging for the whole archive/upload half of the script. STAGE is
# empty until the staging dir exists; the handler removes it when set
# and prints the FAILED marker on any non-zero exit.
STAGE=""
# --- run record (#1026, design #1024 phase 3) --------------------------------
# Every real run (complete or failed) leaves one JSON record at
# $RECORD beside the journal cursor: the artefact the laptop-backups
# watchdog reads to name a stopped or failing backup. The EXIT trap
# below is the only writer — the same single handler as #1010 (a
# second trap would replace it). STEP names the step in progress and
# each destination moves not-attempted -> in-progress -> ok as its leg
# runs; on a failed exit an in-progress destination is recorded
# failed. The single-instance SKIP below is not a run and leaves no
# record (RECORD_SKIP=1). The record lives at the backup root, never
# inside a dated day dir, so neither prune can remove it (the local
# prune iterates only $ROOT/20??-??-??, the remote prunes only dated
# dirs). A failed record carries result=failed, so the watchdog keys
# on finish time + result and a failure can never masquerade as
# freshness.
STEP="validate-date"
DEST_LOCAL="not-attempted"
DEST_R2="not-attempted"
DEST_DRIVE="not-attempted"
ARC_SHA=""
# Suppressed until DATE validation passes: an invalid DATE must be
# rejected before ANY side effect (#989, pinned by the date suite),
# so a rejected run leaves no record and no backup root.
RECORD_SKIP=1
write_record() {
  local rc="$1" result="complete" d_local="$DEST_LOCAL" d_r2="$DEST_R2" d_drive="$DEST_DRIVE"
  if [ "$rc" -ne 0 ]; then
    result="failed"
    [ "$d_local" = "in-progress" ] && d_local="failed"
    [ "$d_r2" = "in-progress" ] && d_r2="failed"
    [ "$d_drive" = "in-progress" ] && d_drive="failed"
  fi
  command -v python3 >/dev/null 2>&1 || return 0
  mkdir -p "$ROOT" 2>/dev/null || return 0
  RECORD_PATH="$RECORD" RECORD_RESULT="$result" RECORD_RC="$rc" \
  RECORD_STEP="$STEP" RECORD_DATE="$DATE" RECORD_STARTED="$CREATED_AT" \
  RECORD_SHA="$ARC_SHA" RECORD_LOCAL="$d_local" RECORD_R2="$d_r2" \
  RECORD_DRIVE="$d_drive" \
  python3 - <<'PYEOF' 2>/dev/null || true
import json, os
from datetime import datetime, timezone
rec = {
    "job": "pm-dashboard-backup",
    "date": os.environ["RECORD_DATE"],
    "started_at": os.environ["RECORD_STARTED"],
    "finished_at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    "result": os.environ["RECORD_RESULT"],
    "exit_code": int(os.environ["RECORD_RC"]),
    "failing_step": os.environ["RECORD_STEP"] if os.environ["RECORD_RESULT"] == "failed" else None,
    "archive_sha256": os.environ["RECORD_SHA"] or None,
    "destinations": {
        "local": os.environ["RECORD_LOCAL"],
        "r2": os.environ["RECORD_R2"],
        "drive": os.environ["RECORD_DRIVE"],
    },
}
tmp = os.environ["RECORD_PATH"] + ".tmp"
with open(tmp, "w") as fh:
    json.dump(rec, fh, indent=2)
    fh.write("\n")
os.replace(tmp, os.environ["RECORD_PATH"])
PYEOF
}
on_exit() {
  rc=$?
  if [ -n "$STAGE" ]; then rm -rf "$STAGE"; fi
  if [ "$RECORD_SKIP" -ne 1 ]; then write_record "$rc"; fi
  if [ $rc -ne 0 ]; then echo "pm-dashboard-backup: FAILED exit=$rc" >&2; fi
}
trap on_exit EXIT

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
RECORD_SKIP=0

# --- single instance -------------------------------------------------------
exec 9>"$ROOT/.pm-dashboard-backup.lock"
if ! flock -n 9; then
  log "SKIP another pm-dashboard-backup run holds the lock"
  RECORD_SKIP=1
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

STEP="credentials"
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
# The r2-backups token cannot HeadBucket (403), so rclone must skip its
# bucket check - otherwise it falls back to CreateBucket and the upload
# fails with AccessDenied (found 2026-10-28 via the openclaw-backup job).
export RCLONE_CONFIG_R2BAK_NO_CHECK_BUCKET="true"


# --- optional pm-stack DB dumps (code#1514) --------------------------------
# The #1514 dump job (Kaneo Postgres + Grist + Pad) publishes its script at
# ~/tools/pm-stack-db-backup.sh and writes dumps to ~/Backups/db-dumps/<date>/
# (here: ~/backups/db-dumps/<date>/). When present it runs first and its
# output rides this archive to all three destinations; until it lands, the
# archive carries the dashboard state set only and says so in the log.
DUMP_SCRIPT="${PM_DB_DUMP_SCRIPT:-$HOME/tools/pm-stack-db-backup.sh}"
DUMP_DIR="$HOME/backups/db-dumps/$DATE"
DUMP_RAN=0
if [ -x "$DUMP_SCRIPT" ]; then
  STEP="db-dump"
  log "running DB dump step: $DUMP_SCRIPT"
  "$DUMP_SCRIPT" "$DATE" || fail "DB dump step failed"
  DUMP_RAN=1
else
  log "no DB dump script at $DUMP_SCRIPT (code#1514 pending) - dashboard state only"
fi

# === leg 1: the local archive (dashboard-backup.sh, code#1459) ============
STEP="local-archive"
DEST_LOCAL="in-progress"
STAGE="$(mktemp -d)"
mkdir -p "$STAGE/aggregator" "$STAGE/meta-quota-export" "$STAGE/journals" "$OUTDIR"

# --- expected set (#1025, design #1024 phase 2) ------------------------------
# The dataset's expected members, declared ONCE here: "<class> <kind>
# <path-in-archive>" per line. The manifest step records every member's
# status (present, with sha256+size for files, or missing) and the
# archive verify step fails the run when a REQUIRED member is missing —
# until now the manifest was generated from whatever staging produced,
# so verification was self-referential and a member deleted/renamed at
# the source silently shrank the backup set. Classes preserve the
# intent the staging code below has always had: existing state files
# are required (their WARN-for-missing becomes a failed run at verify);
# the snapshots tree and the journal logs are optional (journals keep
# their tolerated-unavailable placeholder, but are now recorded);
# db-dumps is CONDITIONALLY required — required when the DB dump step
# ran (DUMP_RAN=1), optional otherwise.
EXPECTED_SET="required file aggregator/actions.json
required file aggregator/comments.json
required file aggregator/fleet-feed.json
required file aggregator/quota-current.json
required file aggregator/registry.json
required file aggregator/quota-gatherer-state.json
required file aggregator/quota-gatherer-heartbeat.json
required file meta-quota-export/latest.json
required file meta-quota-export/history.jsonl
conditional tree db-dumps
optional tree aggregator/snapshots
optional file journals/pm-aggregator.log
optional file journals/quota-gatherer.log
optional file journals/meta-quota-export.log"

# --- data set (explicit list only; the aggregator .env is deliberately NOT
# --- included — no config/secret files belong in this archive) ---
# Every staging copy is checked (#1023): the manifest below is generated
# by walking the staged tree, so an unchecked cp failure (a present but
# unreadable dataset file) was invisible to the manifest verify — the
# run reported full success with an incomplete archive, uploaded it to
# both remotes, and advanced the journal cursor. A failed copy fails
# the run here via fail(), naming the source, before any upload and
# before the cursor can advance. A missing file stays a WARN, as
# before: absent and present-but-unreadable are different events.
for f in actions.json comments.json fleet-feed.json quota-current.json \
         registry.json quota-gatherer-state.json quota-gatherer-heartbeat.json; do
  if [ -f "$AGG/$f" ]; then cp -a "$AGG/$f" "$STAGE/aggregator/$f" \
    || fail "dataset copy failed: $AGG/$f";
  else echo "pm-dashboard-backup: WARN missing $AGG/$f" >&2; fi
done
if [ -d "$AGG/snapshots" ]; then cp -a "$AGG/snapshots" "$STAGE/aggregator/snapshots" \
  || fail "dataset copy failed: $AGG/snapshots"; fi
for f in latest.json history.jsonl; do
  if [ -f "$META/$f" ]; then cp -a "$META/$f" "$STAGE/meta-quota-export/$f" \
    || fail "dataset copy failed: $META/$f";
  else echo "pm-dashboard-backup: WARN missing $META/$f" >&2; fi
done

if [ -d "$DUMP_DIR" ]; then cp -a "$DUMP_DIR" "$STAGE/db-dumps" \
  || fail "dataset copy failed: $DUMP_DIR"; fi

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

# --- manifest (files as staged + the expected set's per-member status) ---
EXPECTED_SET="$EXPECTED_SET" DUMP_RAN="$DUMP_RAN" \
python3 - "$STAGE" "$DATE" "$CREATED_AT" <<'PYEOF'
import hashlib, json, os, sys
stage, date, created = sys.argv[1], sys.argv[2], sys.argv[3]
files = []
by_path = {}
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
        entry = {"path": rel, "sha256": h.hexdigest(), "size": os.path.getsize(p)}
        files.append(entry)
        by_path[rel] = entry
files.sort(key=lambda f: f["path"])
expected = []
for line in os.environ.get("EXPECTED_SET", "").splitlines():
    parts = line.split(None, 2)
    if len(parts) != 3:
        continue
    cls, kind, rel = parts
    if cls == "conditional":
        cls = "required" if os.environ.get("DUMP_RAN") == "1" else "optional"
    member = {"path": rel, "class": cls, "kind": kind}
    if kind == "tree":
        full = os.path.join(stage, rel)
        if os.path.isdir(full):
            member["status"] = "present"
            member["files"] = sum(1 for f in files
                                  if f["path"] == rel or f["path"].startswith(rel + "/"))
        else:
            member["status"] = "missing"
    elif rel in by_path:
        member["status"] = "present"
        member["sha256"] = by_path[rel]["sha256"]
        member["size"] = by_path[rel]["size"]
    else:
        member["status"] = "missing"
    expected.append(member)
with open(os.path.join(stage, "manifest.json"), "w") as fh:
    json.dump({"archive": f"dashboard-{date}", "created_at": created,
               "host": os.uname().nodename, "scope": "laptop",
               "files": files, "expected": expected}, fh, indent=2)
missing_req = [m["path"] for m in expected
               if m["status"] == "missing" and m["class"] == "required"]
print(f"pm-dashboard-backup: manifest covers {len(files)} files, "
      f"{len(expected)} expected members "
      f"({len(expected) - sum(1 for m in expected if m['status'] == 'missing')} present, "
      f"{sum(1 for m in expected if m['status'] == 'missing')} missing, "
      f"{len(missing_req)} required-missing)")
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
# Expected set (#1025): a required member missing from the archive —
# whether the manifest recorded it missing at staging or it is absent
# here despite being recorded present — fails verification, naming
# the member, before anything is uploaded.
for member in m.get("expected", []):
    p = member["path"]
    present = os.path.isdir(p) if member.get("kind") == "tree" else os.path.isfile(p)
    if member["class"] == "required" and (member["status"] == "missing" or not present):
        print(f"pm-dashboard-backup: FAILED required dataset member missing: {p}",
              file=sys.stderr)
        bad += 1
    elif member["status"] == "present" and not present:
        print("MISSING", p); bad += 1
sys.exit(1 if bad else 0)
PYEOF
); then
  echo "pm-dashboard-backup: VERIFY FAILED for $ARCHIVE" >&2
  rm -rf "$VDIR"; exit 1
fi
rm -rf "$VDIR"

ARC_SHA="$(sha256sum "$ARCHIVE" | cut -d' ' -f1)"
DEST_LOCAL="ok"
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
  # A failed listing fails the run, like a failed purge below (#1021):
  # this used to be `2>/dev/null || true`, so an expired-credential /
  # renamed-bucket / network failure read exactly like a fresh remote
  # (empty list, nothing pruned) and the run still exited 0 with the
  # full OK line and an advanced journal cursor. rclone's stderr is
  # deliberately not discarded, so the cause reaches the log. A
  # genuinely empty listing still succeeds: lsf exits 0 with no
  # output and the loop below simply has nothing to prune.
  subs="$(rclone lsf --dirs-only "$1")" \
    || fail "prune listing failed on $2 ($1)"
  for day in $subs; do
    day="${day%/}"
    if [[ "$day" =~ ^20[0-9]{2}-[0-9]{2}-[0-9]{2}$ ]] && [[ "$day" != "$DATE" && "$day" < "$CUTOFF" ]]; then
      rclone purge "$1/$day" >/dev/null 2>&1 || fail "prune failed on $2 ($day)"
      log "pruned $2 $day"
    fi
  done
}

STEP="r2-upload"
DEST_R2="in-progress"
R2_DEST="r2bak:$R2_BUCKET/$R2_PREFIX/$DATE/dashboard-$DATE.tar.zst"
rclone copyto "$ARCHIVE" "$R2_DEST" || fail "R2 upload failed"
verify_remote "$R2_DEST" "R2"
prune_remote "r2bak:$R2_BUCKET/$R2_PREFIX" "R2"
DEST_R2="ok"

STEP="drive-upload"
DEST_DRIVE="in-progress"
DRIVE_DEST="drive:$DRIVE_DIR/$DATE/dashboard-$DATE.tar.zst"
rclone copyto "$ARCHIVE" "$DRIVE_DEST" || fail "Drive upload failed"
verify_remote "$DRIVE_DEST" "Drive"
prune_remote "drive:$DRIVE_DIR" "Drive"
DEST_DRIVE="ok"
STEP="complete"

# --- journal cursor: advance ONLY now that all three legs have succeeded ---
# (#981: this write used to sit right after the local leg, so a run that
# failed on R2/Drive still advanced the cursor and its journal window was
# never included in any later archive.)
echo "$NOW_EPOCH" > "$MARKER"

log "OK dashboard $DATE backed up to local + R2 + Drive (sha256 $ARC_SHA)"
