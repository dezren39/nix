#!/usr/bin/env bash
set -uo pipefail

RUN_TIMESTAMP="$(date +%s)"

# ─── Record original command ─────────────────────────────────────────────────
# Shell-safe reconstruction so the stored command can be copy-pasted.
ORIGINAL_CMD="$0"
for _arg in "$@"; do ORIGINAL_CMD+=" $(printf '%q' "$_arg")"; done
unset _arg

# ─── Defaults ────────────────────────────────────────────────────────────────

DRY_RUN=false
FORCE=false
VERBOSE=false
SIMPLE=false
NO_INPUT=false
SKIP_BACKUP=false
SKIP_CONFLICT=false
STRICT=false
UNDO_TIMESTAMP=""
SKIP_RESTORE=false
INPUT_DIR=""
OUTPUT_DIR=""
BACKUP_SUFFIX=".bak.$RUN_TIMESTAMP"

# Default excludes — override with SYMLINKER_DEFAULT_EXCLUDES (colon-separated)
IFS=':' read -r -a DEFAULT_EXCLUDES <<< "${SYMLINKER_DEFAULT_EXCLUDES:-.DS_Store}"
EXTRA_EXCLUDES=()

# Type filters
ONLY_FILES=false
ONLY_DIRS=false

# Name filters — each is an array; filter mode tracks which flag added them
FILTER_EXACT=()
FILTER_PREFIX=()
FILTER_SUFFIX=()
FILTER_REGEX=()
FILTER_MATCH=()     # substring/contains

# Log directory
LOG_DIR="${XDG_CACHE_HOME:-$HOME/.config}/symlinker"

# ─── Usage ───────────────────────────────────────────────────────────────────

usage() {
  cat <<'EOF'
symlinker — Create symlinks from one directory's children into another

USAGE
  symlinker.sh --input-dir <dir> --output-dir <dir> [options]
  symlinker.sh --undo <timestamp> [options]

REQUIRED (for normal operation)
  --input-dir <dir>       Source directory whose children will be symlinked
  --output-dir <dir>      Target directory where symlinks are created

OPTIONS
  --dry-run               Preview without making any changes
  --force                 Back up conflicts, then create symlinks
  --skip-backup           With --force, delete backups after symlinking
  --skip-conflict         Skip conflicts instead of failing (report only)
  --no-input, --yes, -y   Skip confirmation prompt (use with --force)
  --exclude <name>        Exclude a child name (repeatable, adds to defaults)
  --backup-suffix <sfx>   Backup suffix (default: .bak.<unix-timestamp>)
  --verbose               Show full absolute paths
  --simple                Omit file types, item counts, symlink targets
  --strict                Stop on first error (default: continue and report)
  -h, --help              Show this help

TYPE FILTERS
  --only-files            Only symlink regular files (skip directories)
  --only-dirs             Only symlink directories (skip regular files)

  These are mutually exclusive. Symlinks in the source are classified by
  what they point to (a symlink to a dir counts as a dir, etc.).

NAME FILTERS
  Each filter flag accepts one or more values, consuming all arguments until
  the next --flag. When multiple filter types are used, a name must match at
  least one pattern in EACH type (AND across types, OR within a type).

  --exact <name...>           Match names exactly
  --prefix <str...>           Match names starting with str (alias: --starts-with)
  --starts-with <str...>      Same as --prefix
  --suffix <str...>           Match names ending with str (alias: --ends-with)
  --ends-with <str...>        Same as --suffix
  --regex <pattern...>        Match names by extended regex (bash =~)
  --match <str...>            Match names containing str (substring)

  Examples:
    symlinker.sh ... --match git nix       # children containing "git" or "nix"
    symlinker.sh ... --prefix . --only-files  # dotfiles only
    symlinker.sh ... --suffix .sh .zsh     # shell scripts
    symlinker.sh ... --regex '^\..*rc$'    # dotfiles ending in "rc"
    symlinker.sh ... --exact .zshrc .bashrc
    symlinker.sh ... --match git --suffix config  # contains "git" AND ends in "config"

UNDO
  --undo <timestamp>      Undo a previous run (unlink symlinks, restore backups)
  --skip-restore          With --undo, don't restore backed-up conflicts

  Undo supports the same flags as normal operation:
    --dry-run             Preview what undo would do
    --force               Force restore even if paths are occupied (removes them)
    --verbose             Show full paths
    --simple              Omit details
    --strict              Stop on first error
    --no-input, --yes     Skip confirmation

  Undo generates its own undo file, so you can undo an undo:
    symlinker.sh --undo <original-timestamp>         # undo a run
    symlinker.sh --undo <undo-timestamp>             # undo the undo

  If the original run used --skip-backup, deleted items cannot be restored.
  Undo will report these as irrecoverable and explain why.

UNDO FILE FORMAT (v2)
  Undo files are written to:
    ${XDG_CACHE_HOME:-~/.config}/symlinker/<timestamp>.undo

  Records:
    SYMLINK:<link>\t<target>     Symlink created (undo: remove it)
    BACKUP:<original>:<backup>   Backup created (undo: restore it)
    DELETE:<backup>\t<original>  Backup deleted (undo: irrecoverable)
    RELINK:<link>\t<target>      Symlink to recreate (from undo-of-undo)

  v1 files (LINK:<path>) are still supported for backward compatibility.

EXCLUDES
  By default, these names are excluded: .DS_Store
  Override the defaults with SYMLINKER_DEFAULT_EXCLUDES (colon-separated).
  Use --exclude to add extra names on top of the defaults.

  SYMLINKER_DEFAULT_EXCLUDES=".DS_Store:.git" symlinker.sh ...
  symlinker.sh --exclude node_modules --exclude .env ...

BEHAVIOR
  Scans --input-dir for immediate children and creates symlinks in --output-dir
  pointing to the realpath of each child. Excluded names are silently skipped.
  Existing correct symlinks are skipped.

  Conflicts (different target or not a symlink) cause an error by default.
  Use --skip-conflict to skip them, or --force to back up and replace them.

  --force moves conflicts to <name><backup-suffix> before creating symlinks.
  --force --skip-backup additionally deletes the backups after symlinking.
  --force prompts before acting unless --no-input/--yes/-y is given.

  Errors do not stop the script by default; all operations are attempted and
  errors are reported at the end. Use --strict to stop on first error.

  Logs and undo files are written to:
    ${XDG_CACHE_HOME:-~/.config}/symlinker/<timestamp>.log
    ${XDG_CACHE_HOME:-~/.config}/symlinker/<timestamp>.undo

EXAMPLES
  symlinker.sh --input-dir ~/git --output-dir ~ --dry-run
  symlinker.sh --input-dir ~/git --output-dir ~ --skip-conflict
  symlinker.sh --input-dir ~/git --output-dir ~ --force --dry-run
  symlinker.sh --input-dir ~/git --output-dir ~ --force --yes
  symlinker.sh --input-dir ~/git --output-dir ~ --force --skip-backup --yes
  symlinker.sh --input-dir ~/git --output-dir ~ --exclude .envrc
  symlinker.sh --input-dir ~/git --output-dir ~ --only-dirs
  symlinker.sh --input-dir ~/git --output-dir ~ --match nix git
  symlinker.sh --input-dir ~/git --output-dir ~ --prefix . --only-files
  symlinker.sh --input-dir ~/git --output-dir ~ --regex '^\..*rc$'
  symlinker.sh --undo 1234567890
  symlinker.sh --undo 1234567890 --skip-restore
  symlinker.sh --undo 1234567890 --dry-run
  symlinker.sh --undo 1234567890 --force
  symlinker.sh --undo 1234567890 --verbose --strict
EOF
}

# ─── Parse arguments ─────────────────────────────────────────────────────────

while [[ $# -gt 0 ]]; do
  case "$1" in
    --input-dir)
      [[ -n "${2:-}" ]] || { echo "Error: --input-dir requires a value" >&2; exit 1; }
      INPUT_DIR="$2"; shift 2 ;;
    --output-dir)
      [[ -n "${2:-}" ]] || { echo "Error: --output-dir requires a value" >&2; exit 1; }
      OUTPUT_DIR="$2"; shift 2 ;;
    --backup-suffix)
      [[ -n "${2:-}" ]] || { echo "Error: --backup-suffix requires a value" >&2; exit 1; }
      BACKUP_SUFFIX="$2"; shift 2 ;;
    --undo)
      [[ -n "${2:-}" ]] || { echo "Error: --undo requires a timestamp" >&2; exit 1; }
      UNDO_TIMESTAMP="$2"; shift 2 ;;
    --dry-run)           DRY_RUN=true;       shift ;;
    --force)             FORCE=true;         shift ;;
    --verbose)           VERBOSE=true;       shift ;;
    --simple)            SIMPLE=true;        shift ;;
    --no-input|--yes|-y) NO_INPUT=true;      shift ;;
    --skip-backup)       SKIP_BACKUP=true;   shift ;;
    --skip-conflict)     SKIP_CONFLICT=true; shift ;;
    --skip-restore)      SKIP_RESTORE=true;  shift ;;
    --strict)            STRICT=true;        shift ;;
    --only-files)        ONLY_FILES=true;    shift ;;
    --only-dirs)         ONLY_DIRS=true;     shift ;;
    --exclude)
      [[ -n "${2:-}" ]] || { echo "Error: --exclude requires a value" >&2; exit 1; }
      EXTRA_EXCLUDES+=("$2"); shift 2 ;;
    --exact)
      shift
      while [[ $# -gt 0 && "$1" != --* ]]; do
        FILTER_EXACT+=("$1"); shift
      done
      [[ ${#FILTER_EXACT[@]} -gt 0 ]] || { echo "Error: --exact requires at least one value" >&2; exit 1; }
      ;;
    --prefix|--starts-with)
      shift
      while [[ $# -gt 0 && "$1" != --* ]]; do
        FILTER_PREFIX+=("$1"); shift
      done
      [[ ${#FILTER_PREFIX[@]} -gt 0 ]] || { echo "Error: --prefix/--starts-with requires at least one value" >&2; exit 1; }
      ;;
    --suffix|--ends-with)
      shift
      while [[ $# -gt 0 && "$1" != --* ]]; do
        FILTER_SUFFIX+=("$1"); shift
      done
      [[ ${#FILTER_SUFFIX[@]} -gt 0 ]] || { echo "Error: --suffix/--ends-with requires at least one value" >&2; exit 1; }
      ;;
    --regex)
      shift
      while [[ $# -gt 0 && "$1" != --* ]]; do
        FILTER_REGEX+=("$1"); shift
      done
      [[ ${#FILTER_REGEX[@]} -gt 0 ]] || { echo "Error: --regex requires at least one value" >&2; exit 1; }
      ;;
    --match)
      shift
      while [[ $# -gt 0 && "$1" != --* ]]; do
        FILTER_MATCH+=("$1"); shift
      done
      [[ ${#FILTER_MATCH[@]} -gt 0 ]] || { echo "Error: --match requires at least one value" >&2; exit 1; }
      ;;
    -h|--help)           usage; exit 0 ;;
    *)
      echo "Error: Unknown argument: $1" >&2
      echo >&2
      usage >&2
      exit 1 ;;
  esac
done

# Validate conflicting type filters
if $ONLY_FILES && $ONLY_DIRS; then
  echo "Error: --only-files and --only-dirs are mutually exclusive" >&2; exit 1
fi

# ─── Display helpers (basic — enhanced after validate for forward path) ──────

# Common prefix for shorter display; set to "" until forward path computes it
COMMON_PREFIX=""

# Log directory + file setup for undo (forward path overrides LOG_FILE later)
mkdir -p "$LOG_DIR"
UNDO_LOG_FILE=""

disp() {
  if $VERBOSE; then printf '%s' "$1"; else printf '%s' "${1#"$COMMON_PREFIX"}"; fi
}

describe() {
  $SIMPLE && return 0
  local p="$1"
  if [[ -L "$p" ]]; then
    printf ' (symlink -> %s)' "$(readlink "$p")"
  elif [[ -d "$p" ]]; then
    local n
    n="$(find "$p" -mindepth 1 -maxdepth 1 2>/dev/null | wc -l | tr -d ' ')"
    printf ' (dir, %s items)' "$n"
  elif [[ -f "$p" ]]; then
    printf ' (file)'
  fi
}

ulog() {
  # Log for undo operations — prints and optionally writes to undo log file
  local msg
  if $DRY_RUN; then msg="[DRY-RUN] $*"; else msg="$*"; fi
  echo "$msg"
  [[ -n "$UNDO_LOG_FILE" ]] && echo "$msg" >> "$UNDO_LOG_FILE"
  return 0
}

# ─── Undo operation ─────────────────────────────────────────────────────────

if [[ -n "$UNDO_TIMESTAMP" ]]; then
  UNDO_FILE="$LOG_DIR/$UNDO_TIMESTAMP.undo"
  if [[ ! -f "$UNDO_FILE" ]]; then
    echo "Error: No undo file found for timestamp $UNDO_TIMESTAMP" >&2
    echo "  Expected: $UNDO_FILE" >&2
    echo "  Available:" >&2
    found=false
    for f in "$LOG_DIR"/*.undo; do
      [[ -f "$f" ]] || continue
      found=true
      echo "    $(basename "$f" .undo)" >&2
    done
    $found || echo "    (none)" >&2
    exit 1
  fi

  # ─── Detect undo file format ───────────────────────────────────────────
  UNDO_VERSION=1
  UNDO_ORIG_CMD=""
  UNDO_ORIG_INPUT_DIR=""
  UNDO_ORIG_OUTPUT_DIR=""
  UNDO_ORIG_SKIP_BACKUP=false
  UNDO_ORIG_FORCE=false

  while IFS= read -r hdr; do
    case "$hdr" in
      "# UNDO_VERSION:2") UNDO_VERSION=2 ;;
      "# ORIGINAL_CMD:"*) UNDO_ORIG_CMD="${hdr#"# ORIGINAL_CMD:"}" ;;
      "# input-dir:"*)    UNDO_ORIG_INPUT_DIR="${hdr#"# input-dir: "}" ;;
      "# output-dir:"*)   UNDO_ORIG_OUTPUT_DIR="${hdr#"# output-dir: "}" ;;
      "# skip-backup:"*)  [[ "${hdr#"# skip-backup: "}" == "true" ]] && UNDO_ORIG_SKIP_BACKUP=true ;;
      "# force:"*)        [[ "${hdr#"# force: "}" == "true" ]] && UNDO_ORIG_FORCE=true ;;
      "#"*) ;;  # other comments
      *) break ;;  # first non-comment line — stop parsing header
    esac
  done < "$UNDO_FILE"

  # Set up undo log file
  UNDO_LOG_FILE="$LOG_DIR/${RUN_TIMESTAMP}.undo-log"
  {
    echo "# symlinker undo log — $RUN_TIMESTAMP"
    echo "# undoing: $UNDO_TIMESTAMP"
  } > "$UNDO_LOG_FILE"

  # Set up undo-of-undo file (so this undo can itself be reversed)
  UNDO_OF_UNDO_FILE="$LOG_DIR/${RUN_TIMESTAMP}.undo"
  {
    echo "# UNDO_VERSION:2"
    echo "# symlinker undo — $RUN_TIMESTAMP"
    echo "# ORIGINAL_CMD:$ORIGINAL_CMD"
    echo "# undoing: $UNDO_TIMESTAMP"
  } > "$UNDO_OF_UNDO_FILE"

  # ─── Undo: result tracking ────────────────────────────────────────────
  undo_unlinked_paths=()
  undo_restored_orig=();      undo_restored_bak=()
  undo_relinked_links=();     undo_relinked_targets=()
  undo_already_gone=()
  undo_irrecoverable_paths=(); undo_irrecoverable_reasons=()
  undo_error_paths=();        undo_error_msgs=()
  undo_skipped_restore=()
  UNDO_EXIT_CODE=0

  ulog "=== Undo: $UNDO_TIMESTAMP ==="

  if [[ -n "$UNDO_ORIG_CMD" ]]; then
    ulog "Original command: $UNDO_ORIG_CMD"
  fi

  ulog ""

  # ─── Undo: process records ────────────────────────────────────────────
  while IFS= read -r line; do
    case "$line" in
      "#"*) continue ;;  # skip comments/headers

      LINK:*|SYMLINK:*)
        # v1: LINK:<path>  v2: SYMLINK:<link>\t<target>
        if [[ "$line" == SYMLINK:* ]]; then
          rest="${line#SYMLINK:}"
          link="${rest%%	*}"
          target="${rest#*	}"
        else
          link="${line#LINK:}"
          target=""
        fi

        if $DRY_RUN; then
          ulog "  rm $link"
          undo_unlinked_paths+=("$link")
          # Record for undo-of-undo: recreate the symlink
          if [[ -L "$link" ]]; then
            actual_target="$(readlink "$link")"
            echo "RELINK:${link}	${actual_target}" >> "$UNDO_OF_UNDO_FILE"
          elif [[ -n "$target" ]]; then
            echo "RELINK:${link}	${target}" >> "$UNDO_OF_UNDO_FILE"
          fi
          continue
        fi

        if [[ -L "$link" ]]; then
          actual_target="$(readlink "$link")"
          err_msg=""
          if err_msg="$(rm "$link" 2>&1)"; then
            ulog "  rm $link"
            undo_unlinked_paths+=("$link")
            # Record for undo-of-undo: recreate the symlink
            echo "RELINK:${link}	${actual_target}" >> "$UNDO_OF_UNDO_FILE"
          else
            ulog "  Error: Failed to unlink $link: $err_msg" >&2
            undo_error_paths+=("$link")
            undo_error_msgs+=("unlink failed: $err_msg")
            UNDO_EXIT_CODE=1
            if $STRICT; then break; fi
          fi
        elif [[ -e "$link" ]]; then
          ulog "  Warning: $link exists but is not a symlink, skipping" >&2
          undo_error_paths+=("$link")
          undo_error_msgs+=("exists but is not a symlink")
          UNDO_EXIT_CODE=1
          if $STRICT; then break; fi
        else
          ulog "  Already gone: $link"
          undo_already_gone+=("$link")
        fi
        ;;

      BACKUP:*)
        # v1+v2: BACKUP:<original>:<backup>
        rest="${line#BACKUP:}"
        original="${rest%%:*}"
        backup="${rest#*:}"

        if $SKIP_RESTORE; then
          undo_skipped_restore+=("$original")
          continue
        fi

        if $DRY_RUN; then
          if [[ -e "$backup" ]]; then
            ulog "  mv $backup $original"
            undo_restored_orig+=("$original")
            undo_restored_bak+=("$backup")
            # Record for undo-of-undo: move it back (swap fields)
            echo "BACKUP:${backup}:${original}" >> "$UNDO_OF_UNDO_FILE"
          else
            ulog "  Backup gone: $backup (cannot restore $original)"
            undo_irrecoverable_paths+=("$original")
            undo_irrecoverable_reasons+=("backup file missing: $backup")
          fi
          continue
        fi

        if [[ -e "$backup" ]]; then
          if [[ -e "$original" || -L "$original" ]]; then
            if $FORCE; then
              # Force mode: remove the thing at original path, then restore
              err_msg=""
              if err_msg="$(rm -rf "$original" 2>&1)"; then
                if err_msg="$(mv "$backup" "$original" 2>&1)"; then
                  ulog "  mv $backup $original (forced)"
                  undo_restored_orig+=("$original")
                  undo_restored_bak+=("$backup")
                  echo "BACKUP:${backup}:${original}" >> "$UNDO_OF_UNDO_FILE"
                else
                  ulog "  Error: Failed to restore $backup -> $original: $err_msg" >&2
                  undo_error_paths+=("$original")
                  undo_error_msgs+=("restore failed: $err_msg")
                  UNDO_EXIT_CODE=1
                  if $STRICT; then break; fi
                fi
              else
                ulog "  Error: Failed to remove $original before restore: $err_msg" >&2
                undo_error_paths+=("$original")
                undo_error_msgs+=("remove before restore failed: $err_msg")
                UNDO_EXIT_CODE=1
                if $STRICT; then break; fi
              fi
            else
              ulog "  Warning: $original still exists, cannot restore from $backup (use --force to override)" >&2
              undo_error_paths+=("$original")
              undo_error_msgs+=("still exists, cannot restore")
              UNDO_EXIT_CODE=1
              if $STRICT; then break; fi
            fi
          else
            err_msg=""
            if err_msg="$(mv "$backup" "$original" 2>&1)"; then
              ulog "  mv $backup $original"
              undo_restored_orig+=("$original")
              undo_restored_bak+=("$backup")
              echo "BACKUP:${backup}:${original}" >> "$UNDO_OF_UNDO_FILE"
            else
              ulog "  Error: Failed to restore $backup -> $original: $err_msg" >&2
              undo_error_paths+=("$original")
              undo_error_msgs+=("restore failed: $err_msg")
              UNDO_EXIT_CODE=1
              if $STRICT; then break; fi
            fi
          fi
        else
          ulog "  Backup gone: $backup (cannot restore $(disp "$original"))"
          undo_irrecoverable_paths+=("$original")
          undo_irrecoverable_reasons+=("backup file missing: $backup")
        fi
        ;;

      DELETE:*)
        # v2: DELETE:<backup_path>\t<original_path>
        rest="${line#DELETE:}"
        deleted_bak="${rest%%	*}"
        deleted_orig="${rest#*	}"

        undo_irrecoverable_paths+=("$deleted_orig")
        if $UNDO_ORIG_SKIP_BACKUP; then
          undo_irrecoverable_reasons+=("original ran with --skip-backup; backup was permanently deleted")
        else
          undo_irrecoverable_reasons+=("backup was deleted after symlinking")
        fi
        ;;

      RELINK:*)
        # v2 (from undo-of-undo): RELINK:<link>\t<target>
        # This means "recreate this symlink" — used when undoing an undo
        rest="${line#RELINK:}"
        link="${rest%%	*}"
        target="${rest#*	}"

        if $DRY_RUN; then
          ulog "  ln -s $target $link"
          undo_relinked_links+=("$link")
          undo_relinked_targets+=("$target")
          echo "SYMLINK:${link}	${target}" >> "$UNDO_OF_UNDO_FILE"
          continue
        fi

        if [[ -e "$link" || -L "$link" ]]; then
          if $FORCE; then
            err_msg=""
            if err_msg="$(rm -rf "$link" 2>&1)"; then
              : # removed, proceed to create
            else
              ulog "  Error: Failed to remove $link before relinking: $err_msg" >&2
              undo_error_paths+=("$link")
              undo_error_msgs+=("remove before relink failed: $err_msg")
              UNDO_EXIT_CODE=1
              if $STRICT; then break; fi
              continue
            fi
          else
            ulog "  Warning: $link already exists, cannot relink (use --force to override)" >&2
            undo_error_paths+=("$link")
            undo_error_msgs+=("already exists, cannot relink")
            UNDO_EXIT_CODE=1
            if $STRICT; then break; fi
            continue
          fi
        fi

        err_msg=""
        if err_msg="$(ln -s "$target" "$link" 2>&1)"; then
          ulog "  ln -s $target $link"
          undo_relinked_links+=("$link")
          undo_relinked_targets+=("$target")
          # undo-of-undo: remove this symlink again
          echo "SYMLINK:${link}	${target}" >> "$UNDO_OF_UNDO_FILE"
        else
          ulog "  Error: Failed to relink $link -> $target: $err_msg" >&2
          undo_error_paths+=("$link")
          undo_error_msgs+=("relink failed: $err_msg")
          UNDO_EXIT_CODE=1
          if $STRICT; then break; fi
        fi
        ;;
    esac
  done < "$UNDO_FILE"

  # ─── Undo: print results ──────────────────────────────────────────────

  # Unlinked (first 8 + more)
  if (( ${#undo_unlinked_paths[@]} > 0 )); then
    ulog ""
    if $DRY_RUN; then ulog "Would unlink:"; else ulog "Unlinked:"; fi
    local_show=$(( ${#undo_unlinked_paths[@]} < 8 ? ${#undo_unlinked_paths[@]} : 8 ))
    for (( j=0; j<local_show; j++ )); do
      ulog "  ${undo_unlinked_paths[$j]}"
    done
    local_rem=$(( ${#undo_unlinked_paths[@]} - local_show ))
    if (( local_rem > 0 )); then
      ulog "  ... and ${local_rem} more"
    fi
  fi

  # Restored (first 8 + more)
  if (( ${#undo_restored_orig[@]} > 0 )); then
    ulog ""
    if $DRY_RUN; then ulog "Would restore:"; else ulog "Restored:"; fi
    local_show=$(( ${#undo_restored_orig[@]} < 8 ? ${#undo_restored_orig[@]} : 8 ))
    for (( j=0; j<local_show; j++ )); do
      ulog "  ${undo_restored_bak[$j]} -> ${undo_restored_orig[$j]}"
    done
    local_rem=$(( ${#undo_restored_orig[@]} - local_show ))
    if (( local_rem > 0 )); then
      ulog "  ... and ${local_rem} more"
    fi
  fi

  # Relinked (recreated symlinks from undo-of-undo)
  if (( ${#undo_relinked_links[@]} > 0 )); then
    ulog ""
    if $DRY_RUN; then ulog "Would recreate symlinks:"; else ulog "Recreated symlinks:"; fi
    local_show=$(( ${#undo_relinked_links[@]} < 8 ? ${#undo_relinked_links[@]} : 8 ))
    for (( j=0; j<local_show; j++ )); do
      ulog "  ${undo_relinked_links[$j]} -> ${undo_relinked_targets[$j]}"
    done
    local_rem=$(( ${#undo_relinked_links[@]} - local_show ))
    if (( local_rem > 0 )); then
      ulog "  ... and ${local_rem} more"
    fi
  fi

  # Already gone
  if (( ${#undo_already_gone[@]} > 0 )); then
    ulog ""
    ulog "Already gone:"
    for p in "${undo_already_gone[@]}"; do
      ulog "  $p"
    done
  fi

  # Skipped restore
  if (( ${#undo_skipped_restore[@]} > 0 )); then
    ulog ""
    ulog "Skipped restore (--skip-restore):"
    for p in "${undo_skipped_restore[@]}"; do
      ulog "  $p"
    done
  fi

  # Irrecoverable
  if (( ${#undo_irrecoverable_paths[@]} > 0 )); then
    ulog ""
    ulog "Could not restore:"
    for j in "${!undo_irrecoverable_paths[@]}"; do
      ulog "  ${undo_irrecoverable_paths[$j]}: ${undo_irrecoverable_reasons[$j]}"
    done
  fi

  # Errors
  if (( ${#undo_error_paths[@]} > 0 )); then
    ulog ""
    ulog "Errors:"
    for j in "${!undo_error_paths[@]}"; do
      ulog "  ${undo_error_paths[$j]}: ${undo_error_msgs[$j]}"
    done
  fi

  # Summary line
  ulog ""
  undo_summary="${#undo_unlinked_paths[@]} unlinked"
  (( ${#undo_restored_orig[@]} > 0 )) && undo_summary+=", ${#undo_restored_orig[@]} restored"
  (( ${#undo_relinked_links[@]} > 0 )) && undo_summary+=", ${#undo_relinked_links[@]} relinked"
  (( ${#undo_already_gone[@]} > 0 )) && undo_summary+=", ${#undo_already_gone[@]} already gone"
  (( ${#undo_skipped_restore[@]} > 0 )) && undo_summary+=", ${#undo_skipped_restore[@]} restore skipped"
  (( ${#undo_irrecoverable_paths[@]} > 0 )) && undo_summary+=", ${#undo_irrecoverable_paths[@]} irrecoverable"
  (( ${#undo_error_paths[@]} > 0 )) && undo_summary+=", ${#undo_error_paths[@]} error(s)"
  if $DRY_RUN; then
    ulog "[DRY-RUN] $undo_summary"
  else
    ulog "$undo_summary"
  fi

  # Undo-of-undo info
  if ! $DRY_RUN; then
    ulog ""
    ulog "Undo log: $UNDO_LOG_FILE"
    ulog "To undo this undo:"
    ulog ""
    ulog "  $0 --undo $RUN_TIMESTAMP"
    ulog ""
  fi

  if (( UNDO_EXIT_CODE != 0 )); then
    ulog "Completed with errors."
    ulog ""
  fi

  exit $UNDO_EXIT_CODE
fi

# ─── Validate ────────────────────────────────────────────────────────────────

if [[ -z "$INPUT_DIR" ]]; then
  echo "Error: --input-dir is required" >&2; echo >&2; usage >&2; exit 1
fi
if [[ -z "$OUTPUT_DIR" ]]; then
  echo "Error: --output-dir is required" >&2; echo >&2; usage >&2; exit 1
fi
if $SKIP_BACKUP && ! $FORCE; then
  echo "Error: --skip-backup requires --force" >&2; exit 1
fi

INPUT_DIR="$(cd "$INPUT_DIR" 2>/dev/null && pwd -P)" || {
  echo "Error: Cannot access --input-dir: $INPUT_DIR" >&2; exit 1
}
OUTPUT_DIR="$(cd "$OUTPUT_DIR" 2>/dev/null && pwd -P)" || {
  echo "Error: Cannot access --output-dir: $OUTPUT_DIR" >&2; exit 1
}

# ─── Log setup ───────────────────────────────────────────────────────────────

LOG_FILE="$LOG_DIR/$RUN_TIMESTAMP.log"

{
  echo "# symlinker log — $RUN_TIMESTAMP"
  echo "# input-dir: $INPUT_DIR"
  echo "# output-dir: $OUTPUT_DIR"
  echo "# flags: dry-run=$DRY_RUN force=$FORCE skip-backup=$SKIP_BACKUP skip-conflict=$SKIP_CONFLICT strict=$STRICT"
  echo "# type-filter: only-files=$ONLY_FILES only-dirs=$ONLY_DIRS"
  if (( ${#FILTER_EXACT[@]} + ${#FILTER_PREFIX[@]} + ${#FILTER_SUFFIX[@]} + ${#FILTER_REGEX[@]} + ${#FILTER_MATCH[@]} > 0 )); then
    echo "# name-filters: exact=(${FILTER_EXACT[*]:-}) prefix=(${FILTER_PREFIX[*]:-}) suffix=(${FILTER_SUFFIX[*]:-}) regex=(${FILTER_REGEX[*]:-}) match=(${FILTER_MATCH[*]:-})"
  fi
  echo ""
} > "$LOG_FILE"

# ─── Display helpers (forward path — enhance COMMON_PREFIX) ──────────────────

# Compute longest common path prefix for shorter display (overrides the "" default)
if ! $VERBOSE; then
  a="$INPUT_DIR/"
  b="$OUTPUT_DIR/"
  max=$(( ${#a} < ${#b} ? ${#a} : ${#b} ))
  i=0
  while (( i < max )) && [[ "${a:$i:1}" == "${b:$i:1}" ]]; do (( i++ )) || true; done
  COMMON_PREFIX="${a:0:$i}"
  # trim to last / so we don't split mid-component
  COMMON_PREFIX="${COMMON_PREFIX%${COMMON_PREFIX##*/}}"
fi

log() {
  local msg
  if $DRY_RUN; then msg="[DRY-RUN] $*"; else msg="$*"; fi
  echo "$msg"
  echo "$msg" >> "$LOG_FILE"
}

# ─── Name/type filter helpers ────────────────────────────────────────────────

HAS_NAME_FILTERS=false
if (( ${#FILTER_EXACT[@]} + ${#FILTER_PREFIX[@]} + ${#FILTER_SUFFIX[@]} + ${#FILTER_REGEX[@]} + ${#FILTER_MATCH[@]} > 0 )); then
  HAS_NAME_FILTERS=true
fi

# Returns 0 (true) if the name passes all active filters, 1 otherwise.
# When multiple filter types are active, a name must match at least one
# pattern in EACH active type (AND across types, OR within a type).
name_matches_filters() {
  local name="$1"
  $HAS_NAME_FILTERS || return 0  # no filters = everything passes

  if (( ${#FILTER_EXACT[@]} > 0 )); then
    local found=false
    for pat in "${FILTER_EXACT[@]}"; do
      [[ "$name" == "$pat" ]] && { found=true; break; }
    done
    $found || return 1
  fi

  if (( ${#FILTER_PREFIX[@]} > 0 )); then
    local found=false
    for pat in "${FILTER_PREFIX[@]}"; do
      [[ "$name" == "$pat"* ]] && { found=true; break; }
    done
    $found || return 1
  fi

  if (( ${#FILTER_SUFFIX[@]} > 0 )); then
    local found=false
    for pat in "${FILTER_SUFFIX[@]}"; do
      [[ "$name" == *"$pat" ]] && { found=true; break; }
    done
    $found || return 1
  fi

  if (( ${#FILTER_REGEX[@]} > 0 )); then
    local found=false
    for pat in "${FILTER_REGEX[@]}"; do
      [[ "$name" =~ $pat ]] && { found=true; break; }
    done
    $found || return 1
  fi

  if (( ${#FILTER_MATCH[@]} > 0 )); then
    local found=false
    for pat in "${FILTER_MATCH[@]}"; do
      [[ "$name" == *"$pat"* ]] && { found=true; break; }
    done
    $found || return 1
  fi

  return 0
}

# Returns 0 (true) if the entry passes the type filter.
type_matches_filter() {
  local entry="$1"
  if $ONLY_FILES; then
    [[ -f "$entry" && ! -d "$entry" ]] && return 0
    # Also accept symlinks that point to files
    [[ -L "$entry" ]] && [[ -f "$(readlink -f "$entry" 2>/dev/null)" ]] && return 0
    return 1
  fi
  if $ONLY_DIRS; then
    [[ -d "$entry" && ! -L "$entry" ]] && return 0
    # Also accept symlinks that point to dirs
    [[ -L "$entry" ]] && [[ -d "$(readlink -f "$entry" 2>/dev/null)" ]] && return 0
    return 1
  fi
  return 0
}

# ─── Scan and categorize ────────────────────────────────────────────────────

# Build combined excludes set (defaults + extras)
declare -A EXCLUDES_MAP
for e in "${DEFAULT_EXCLUDES[@]}"; do EXCLUDES_MAP["$e"]=1; done
for e in "${EXTRA_EXCLUDES[@]}"; do EXCLUDES_MAP["$e"]=1; done

excluded_count=0
filtered_count=0

skip_links=();     skip_targets=()
create_links=();   create_targets=()
conflict_links=(); conflict_targets=()

while IFS= read -r -d '' entry; do
  name="$(basename "$entry")"

  # Skip excluded names
  if [[ -n "${EXCLUDES_MAP["$name"]+x}" ]]; then
    (( excluded_count++ )) || true
    continue
  fi

  # Apply type filter
  if ! type_matches_filter "$entry"; then
    (( filtered_count++ )) || true
    continue
  fi

  # Apply name filters
  if ! name_matches_filters "$name"; then
    (( filtered_count++ )) || true
    continue
  fi

  target="$(realpath "$entry" 2>/dev/null)" || continue
  link="$OUTPUT_DIR/$name"

  if [[ -L "$link" ]]; then
    existing="$(realpath "$link" 2>/dev/null || readlink "$link")"
    if [[ "$existing" == "$target" ]]; then
      skip_links+=("$link");      skip_targets+=("$target")
    else
      conflict_links+=("$link");  conflict_targets+=("$target")
    fi
  elif [[ -e "$link" ]]; then
    conflict_links+=("$link");    conflict_targets+=("$target")
  else
    create_links+=("$link");      create_targets+=("$target")
  fi
done < <(find "$INPUT_DIR" -mindepth 1 -maxdepth 1 -print0 | sort -z)

total=$(( ${#skip_links[@]} + ${#create_links[@]} + ${#conflict_links[@]} ))
if (( total == 0 )); then
  if $HAS_NAME_FILTERS || $ONLY_FILES || $ONLY_DIRS; then
    log "Nothing matched in $(disp "$INPUT_DIR") (${filtered_count} filtered out, ${excluded_count} excluded)."
  else
    log "Nothing found in $(disp "$INPUT_DIR")."
  fi
  exit 0
fi

# Show active filters
if $HAS_NAME_FILTERS || $ONLY_FILES || $ONLY_DIRS; then
  filter_desc=""
  $ONLY_FILES && filter_desc+="only-files "
  $ONLY_DIRS && filter_desc+="only-dirs "
  (( ${#FILTER_EXACT[@]} > 0 )) && filter_desc+="exact(${FILTER_EXACT[*]}) "
  (( ${#FILTER_PREFIX[@]} > 0 )) && filter_desc+="prefix(${FILTER_PREFIX[*]}) "
  (( ${#FILTER_SUFFIX[@]} > 0 )) && filter_desc+="suffix(${FILTER_SUFFIX[*]}) "
  (( ${#FILTER_REGEX[@]} > 0 )) && filter_desc+="regex(${FILTER_REGEX[*]}) "
  (( ${#FILTER_MATCH[@]} > 0 )) && filter_desc+="match(${FILTER_MATCH[*]}) "
  log "Filters: ${filter_desc% }"
  log ""
fi

# ─── Report: Skipped ────────────────────────────────────────────────────────

if (( ${#skip_links[@]} > 0 )); then
  log "=== Skipped (already correct) ==="
  for i in "${!skip_links[@]}"; do
    log "  $(disp "${skip_links[$i]}") ->"
    log "    $(disp "${skip_targets[$i]}")$(describe "${skip_targets[$i]}")"
    log ""
  done
fi

# ─── Report: To create ──────────────────────────────────────────────────────

if (( ${#create_links[@]} > 0 )); then
  if $DRY_RUN; then log "=== Would create ==="; else log "=== Will create ==="; fi
  for i in "${!create_links[@]}"; do
    log "  $(disp "${create_links[$i]}") ->"
    log "    $(disp "${create_targets[$i]}")$(describe "${create_targets[$i]}")"
    log ""
  done
fi

# ─── Report: Conflicts ──────────────────────────────────────────────────────

if (( ${#conflict_links[@]} > 0 )); then
  if $FORCE; then
    if $SKIP_BACKUP; then
      if $DRY_RUN; then
        log "=== Conflicts (would be DELETED) ==="
      else
        log "=== Conflicts (will be DELETED) ==="
      fi
    else
      if $DRY_RUN; then
        log "=== Conflicts (would be backed up) ==="
      else
        log "=== Conflicts (will be backed up) ==="
      fi
    fi
    for i in "${!conflict_links[@]}"; do
      lnk="${conflict_links[$i]}"
      tgt="${conflict_targets[$i]}"
      if $SKIP_BACKUP; then
        log "  $(disp "$lnk")$(describe "$lnk") => DELETED"
      else
        log "  $(disp "$lnk")$(describe "$lnk") =>"
        log "    $(disp "$lnk")${BACKUP_SUFFIX}"
      fi
      log "  $(disp "$lnk") ->"
      log "    $(disp "$tgt")$(describe "$tgt")"
      log ""
    done
  elif $SKIP_CONFLICT; then
    log "=== Conflicts (skipped) ==="
    for i in "${!conflict_links[@]}"; do
      lnk="${conflict_links[$i]}"
      tgt="${conflict_targets[$i]}"
      log "  $(disp "$lnk")$(describe "$lnk") !="
      log "    $(disp "$tgt")$(describe "$tgt")"
      log ""
    done
  else
    log "=== Conflicts (blocking) ==="
    for i in "${!conflict_links[@]}"; do
      lnk="${conflict_links[$i]}"
      tgt="${conflict_targets[$i]}"
      log "  $(disp "$lnk")$(describe "$lnk") !="
      log "    $(disp "$tgt")$(describe "$tgt")"
      log ""
    done
  fi
fi

# ─── Summary ─────────────────────────────────────────────────────────────────

summary_line="Summary: ${#skip_links[@]} skipped, ${#create_links[@]} to create, ${#conflict_links[@]} conflicts, ${excluded_count} excluded"
(( filtered_count > 0 )) && summary_line+=", ${filtered_count} filtered"
log "$summary_line"

# ─── Conflict error (default) ───────────────────────────────────────────────

if (( ${#conflict_links[@]} > 0 )) && ! $FORCE && ! $SKIP_CONFLICT; then
  log ""
  log "Error: ${#conflict_links[@]} conflict(s) found. Aborting."
  log ""
  log "To resolve:"
  log "  --force                Back up conflicts, then create symlinks"
  log "  --force --skip-backup  Replace conflicts (permanently deletes originals)"
  log "  --skip-conflict        Skip conflicts, create non-conflicting only"
  log ""
  log "Add --no-input, --yes, or -y to skip confirmation prompts."
  exit 1
fi

# ─── Force confirmation ─────────────────────────────────────────────────────

force_confirmed=false
if $FORCE && (( ${#conflict_links[@]} > 0 )); then
  log ""
  if $NO_INPUT; then
    force_confirmed=true
  else
    if $SKIP_BACKUP; then
      if $DRY_RUN; then
        read -r -p "[DRY-RUN] This would PERMANENTLY DELETE ${#conflict_links[@]} conflicting item(s). Continue? [y/N] " answer
      else
        read -r -p "Are you sure? This will PERMANENTLY DELETE ${#conflict_links[@]} conflicting item(s). [y/N] " answer
      fi
    else
      if $DRY_RUN; then
        read -r -p "[DRY-RUN] This would move ${#conflict_links[@]} item(s) to ${BACKUP_SUFFIX} suffix. Continue? [y/N] " answer
      else
        read -r -p "Are you sure? This will move ${#conflict_links[@]} item(s) to ${BACKUP_SUFFIX} suffix. [y/N] " answer
      fi
    fi
    if [[ "$answer" =~ ^[Yy]$ ]]; then
      force_confirmed=true
    else
      echo "Aborted. No changes made."
      exit 1
    fi
  fi
fi

# ─── Result tracking ─────────────────────────────────────────────────────────

done_linked_links=();   done_linked_targets=()
done_backed_up_orig=(); done_backed_up_bak=()
done_deleted_paths=()
done_error_paths=();    done_error_msgs=()
EXIT_CODE=0

print_results() {
  local prefix=""
  $DRY_RUN && prefix="[DRY-RUN] "

  # Symlinked (first 8 + "and X more")
  if (( ${#done_linked_links[@]} > 0 )); then
    log ""
    if $DRY_RUN; then log "Would symlink:"; else log "Symlinked:"; fi
    local show=$(( ${#done_linked_links[@]} < 8 ? ${#done_linked_links[@]} : 8 ))
    for (( j=0; j<show; j++ )); do
      log "  $(disp "${done_linked_links[$j]}") -> $(disp "${done_linked_targets[$j]}")"
    done
    local remaining=$(( ${#done_linked_links[@]} - show ))
    if (( remaining > 0 )); then
      log "  ... and ${remaining} more"
    fi
  fi

  # Skipped (first 8 + "and X more") — from scan phase
  if (( ${#skip_links[@]} > 0 )); then
    log ""
    log "Skipped (already correct):"
    local show=$(( ${#skip_links[@]} < 8 ? ${#skip_links[@]} : 8 ))
    for (( j=0; j<show; j++ )); do
      log "  $(disp "${skip_links[$j]}") -> $(disp "${skip_targets[$j]}")"
    done
    local remaining=$(( ${#skip_links[@]} - show ))
    if (( remaining > 0 )); then
      log "  ... and ${remaining} more"
    fi
  fi

  # All backed up
  if (( ${#done_backed_up_orig[@]} > 0 )); then
    log ""
    if $DRY_RUN; then log "Would back up:"; else log "Backed up:"; fi
    for j in "${!done_backed_up_orig[@]}"; do
      log "  $(disp "${done_backed_up_orig[$j]}") -> $(disp "${done_backed_up_bak[$j]}")"
    done
  fi

  # All deleted (backed up then removed)
  if (( ${#done_deleted_paths[@]} > 0 )); then
    log ""
    if $DRY_RUN; then log "Would delete (back up then remove):"; else log "Deleted (backed up then removed):"; fi
    for p in "${done_deleted_paths[@]}"; do
      log "  $(disp "$p")"
    done
  fi

  # All errors
  if (( ${#done_error_paths[@]} > 0 )); then
    log ""
    log "Errors:"
    for j in "${!done_error_paths[@]}"; do
      if [[ -n "${done_error_msgs[$j]:-}" ]]; then
        log "  $(disp "${done_error_paths[$j]}"): ${done_error_msgs[$j]}"
      else
        log "  $(disp "${done_error_paths[$j]}")"
      fi
    done
  fi

  # Final summary line
  log ""
  local summary="${#done_linked_links[@]} symlinked, ${#skip_links[@]} skipped"
  (( ${#done_backed_up_orig[@]} > 0 )) && summary+=", ${#done_backed_up_orig[@]} backed up"
  (( ${#done_deleted_paths[@]} > 0 )) && summary+=", ${#done_deleted_paths[@]} deleted"
  (( ${#done_error_paths[@]} > 0 )) && summary+=", ${#done_error_paths[@]} error(s)"
  summary+=", ${excluded_count} excluded"
  (( filtered_count > 0 )) && summary+=", ${filtered_count} filtered"
  if $DRY_RUN; then
    log "${prefix}${summary}"
  else
    log "$summary"
  fi
}

# ─── Execute ─────────────────────────────────────────────────────────────────

if $DRY_RUN; then
  log ""

  for i in "${!create_links[@]}"; do
    log "ln -s $(disp "${create_targets[$i]}") $(disp "${create_links[$i]}")"
    done_linked_links+=("${create_links[$i]}")
    done_linked_targets+=("${create_targets[$i]}")
  done

  if $force_confirmed; then
    for i in "${!conflict_links[@]}"; do
      lnk="${conflict_links[$i]}"
      tgt="${conflict_targets[$i]}"
      log "mv $(disp "$lnk") $(disp "$lnk")${BACKUP_SUFFIX}"
      done_backed_up_orig+=("$lnk")
      done_backed_up_bak+=("${lnk}${BACKUP_SUFFIX}")
      log "ln -s $(disp "$tgt") $(disp "$lnk")"
      done_linked_links+=("$lnk")
      done_linked_targets+=("$tgt")
    done
    if $SKIP_BACKUP; then
      for i in "${!conflict_links[@]}"; do
        lnk="${conflict_links[$i]}"
        bak="${lnk}${BACKUP_SUFFIX}"
        log "rm -rf $(disp "$lnk")${BACKUP_SUFFIX}"
        done_deleted_paths+=("$bak")
      done
    fi
  fi

  print_results

  log ""
  log "Log: $LOG_FILE"
else
  # Write undo file header (v2 format)
  UNDO_FILE="$LOG_DIR/$RUN_TIMESTAMP.undo"
  {
    echo "# UNDO_VERSION:2"
    echo "# symlinker undo — $RUN_TIMESTAMP"
    echo "# ORIGINAL_CMD:$ORIGINAL_CMD"
    echo "# input-dir: $INPUT_DIR"
    echo "# output-dir: $OUTPUT_DIR"
    echo "# force: $FORCE"
    echo "# skip-backup: $SKIP_BACKUP"
    echo "# skip-conflict: $SKIP_CONFLICT"
    echo "# strict: $STRICT"
  } > "$UNDO_FILE"

  log ""

  for i in "${!create_links[@]}"; do
    err_msg=""
    if err_msg="$(ln -s "${create_targets[$i]}" "${create_links[$i]}" 2>&1)"; then
      echo "SYMLINK:${create_links[$i]}	${create_targets[$i]}" >> "$UNDO_FILE"
      done_linked_links+=("${create_links[$i]}")
      done_linked_targets+=("${create_targets[$i]}")
    else
      done_error_paths+=("${create_links[$i]}")
      done_error_msgs+=("$err_msg")
      EXIT_CODE=1
      if $STRICT; then break; fi
    fi
  done

  if $force_confirmed && { ! $STRICT || (( EXIT_CODE == 0 )); }; then
    deletable_orig=()
    deletable_bak=()

    for i in "${!conflict_links[@]}"; do
      lnk="${conflict_links[$i]}"
      tgt="${conflict_targets[$i]}"
      bak="${lnk}${BACKUP_SUFFIX}"

      # Back up the conflict
      err_msg=""
      if err_msg="$(mv "$lnk" "$bak" 2>&1)"; then
        done_backed_up_orig+=("$lnk")
        done_backed_up_bak+=("$bak")
      else
        done_error_paths+=("$lnk")
        done_error_msgs+=("backup failed: $err_msg")
        EXIT_CODE=1
        if $STRICT; then break; fi
        continue
      fi

      # Create the symlink
      err_msg=""
      if err_msg="$(ln -s "$tgt" "$lnk" 2>&1)"; then
        echo "SYMLINK:${lnk}	${tgt}" >> "$UNDO_FILE"
        done_linked_links+=("$lnk")
        done_linked_targets+=("$tgt")
        if ! $SKIP_BACKUP; then
          echo "BACKUP:$lnk:$bak" >> "$UNDO_FILE"
        fi
        deletable_orig+=("$lnk")
        deletable_bak+=("$bak")
      else
        # Symlink failed after backup — record backup in undo for restore
        echo "BACKUP:$lnk:$bak" >> "$UNDO_FILE"
        done_error_paths+=("$lnk")
        done_error_msgs+=("symlink failed after backup: $err_msg")
        EXIT_CODE=1
        if $STRICT; then break; fi
      fi
    done

    if $SKIP_BACKUP && { ! $STRICT || (( EXIT_CODE == 0 )); }; then
      for i in "${!deletable_bak[@]}"; do
        bak="${deletable_bak[$i]}"
        orig="${deletable_orig[$i]}"
        err_msg=""
        if err_msg="$(rm -rf "$bak" 2>&1)"; then
          echo "DELETE:${bak}	${orig}" >> "$UNDO_FILE"
          done_deleted_paths+=("$bak")
        else
          # Deletion failed — record backup in undo so it can be restored
          echo "BACKUP:$orig:$bak" >> "$UNDO_FILE"
          done_error_paths+=("$bak")
          done_error_msgs+=("delete failed: $err_msg")
          EXIT_CODE=1
          if $STRICT; then break; fi
        fi
      done
    fi
  fi

  print_results

  log ""
  log "Log: $LOG_FILE"
  log "Undo: $UNDO_FILE"
  log ""
  log "Original command:"
  log "  $ORIGINAL_CMD"

  log ""
  log "To undo this run:"
  log ""
  log "  $0 --undo $RUN_TIMESTAMP"
  log ""

  if (( EXIT_CODE != 0 )); then
    log "Completed with errors. Undo file contains completed operations only."
    log ""
  fi

  exit $EXIT_CODE
fi
