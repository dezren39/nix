#!/usr/bin/env bash
set -euo pipefail

PROG="lootbox-link"
SOURCE="${LOOTBOX_LINK_SOURCE:-/Users/drewry.pope/.config/nix/.lootbox}"
MODE="link"
DRY_RUN=false
TARGET=""

usage() {
  cat <<EOF
Usage: $PROG [--force | --migrate] [--dry-run] [DIR]

Link DIR/.lootbox to the shared Lootbox directory and add .lootbox to
DIR/.gitignore. DIR defaults to the current directory.

Options:
  --force, force       Repoint an existing symlink. Refuses real directories.
  --migrate, migrate   Merge a real .lootbox directory into the shared one,
                       then replace it with the symlink.
  --dry-run, -n        Show the changes without writing anything.
  --source DIR         Override the shared Lootbox directory.
  --help, -h           Show this help.

Migration keeps the newest conflicting file at its original relative path,
stores older differing versions with _YYYY-MM-DD suffixes, and skips files
whose contents are identical.

Environment:
  LOOTBOX_LINK_SOURCE  Override the shared directory.
EOF
}

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

log() {
  printf '%s\n' "$*"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --force | force)
      [[ "$MODE" == "link" ]] || die "--force and --migrate are mutually exclusive"
      MODE="force"
      shift
      ;;
    --migrate | migrate)
      [[ "$MODE" == "link" ]] || die "--force and --migrate are mutually exclusive"
      MODE="migrate"
      shift
      ;;
    --dry-run | -n)
      DRY_RUN=true
      shift
      ;;
    --source)
      [[ $# -ge 2 ]] || die "--source requires a directory"
      SOURCE="$2"
      shift 2
      ;;
    --help | -h)
      usage
      exit 0
      ;;
    --)
      shift
      [[ $# -le 1 ]] || die "expected at most one target directory"
      TARGET="${1:-}"
      break
      ;;
    -*)
      die "unknown option: $1"
      ;;
    *)
      [[ -z "$TARGET" ]] || die "expected at most one target directory"
      TARGET="$1"
      shift
      ;;
  esac
done

TARGET="${TARGET:-.}"
[[ -d "$SOURCE" ]] || die "shared Lootbox directory not found: $SOURCE"
[[ ! -L "$SOURCE" ]] || die "shared Lootbox directory must not be a symlink: $SOURCE"
[[ -d "$TARGET" ]] || die "target directory not found: $TARGET"

SOURCE="$(cd "$SOURCE" && pwd -P)"
TARGET="$(cd "$TARGET" && pwd -P)"
LINK="$TARGET/.lootbox"

git -C "$TARGET" rev-parse --is-inside-work-tree >/dev/null 2>&1 ||
  die "target is not inside a Git worktree: $TARGET"

case "$TARGET/" in
  "$SOURCE/"*) die "target must not be inside the shared Lootbox directory" ;;
esac
[[ "$LINK" != "$SOURCE" ]] || die "refusing to replace the shared Lootbox directory"

has_ignore_entry() {
  local ignore="$TARGET/.gitignore"
  local line
  [[ -f "$ignore" ]] || return 1
  while IFS= read -r line || [[ -n "$line" ]]; do
    case "$line" in
      .lootbox | .lootbox/ | /.lootbox | /.lootbox/) return 0 ;;
    esac
  done < "$ignore"
  return 1
}

add_ignore_entry() {
  local ignore="$TARGET/.gitignore"
  local last_byte
  has_ignore_entry && return 0
  if $DRY_RUN; then
    log "Would add .lootbox to $ignore"
    return 0
  fi
  if [[ -s "$ignore" ]]; then
    last_byte="$(tail -c 1 "$ignore" | od -An -t u1 | tr -d '[:space:]')"
    [[ "$last_byte" == "10" ]] || printf '\n' >> "$ignore"
  fi
  printf '.lootbox\n' >> "$ignore"
  log "Added .lootbox to $ignore"
}

create_link() {
  ln -s "$SOURCE" "$LINK" || return 1
  if [[ ! -L "$LINK" || "$(readlink "$LINK")" != "$SOURCE" ]]; then
    rm -f "$LINK"
    return 1
  fi
  log "Linked $LINK -> $SOURCE"
}

variant_path() {
  local relative_path="$1"
  local epoch="$2"
  local directory base stem extension day candidate counter
  directory="$(dirname "$relative_path")"
  base="$(basename "$relative_path")"
  stem="$base"
  extension=""
  if [[ "$base" == *.* && "$base" != .* ]]; then
    stem="${base%.*}"
    extension=".${base##*.}"
  fi
  day="$(date -u -d "@$epoch" +%F)"
  candidate="$directory/${stem}_${day}${extension}"
  counter=2
  while [[ -e "$STAGING/$candidate" || -L "$STAGING/$candidate" ]]; do
    if [[ -f "$MIGRATING_ITEM" && -f "$STAGING/$candidate" ]] &&
      cmp -s "$MIGRATING_ITEM" "$STAGING/$candidate"; then
      printf '%s\n' ""
      return 0
    fi
    if [[ -L "$MIGRATING_ITEM" && -L "$STAGING/$candidate" ]] &&
      [[ "$(readlink "$MIGRATING_ITEM")" == "$(readlink "$STAGING/$candidate")" ]]; then
      printf '%s\n' ""
      return 0
    fi
    candidate="$directory/${stem}_${day}_${counter}${extension}"
    counter=$((counter + 1))
  done
  printf '%s\n' "$candidate"
}

copy_variant() {
  local item="$1"
  local relative_path="$2"
  local epoch="$3"
  local variant
  MIGRATING_ITEM="$item"
  variant="$(variant_path "$relative_path" "$epoch")"
  [[ -n "$variant" ]] || return 0
  mkdir -p "$STAGING/$(dirname "$variant")"
  cp -a "$item" "$STAGING/$variant"
}

merge_directory() {
  local local_root="$1"
  local item relative_path destination source_mtime destination_mtime

  while IFS= read -r -d '' item; do
    relative_path="${item#"$local_root"/}"
    destination="$STAGING/$relative_path"

    if [[ -d "$item" && ! -L "$item" ]]; then
      if [[ -e "$destination" || -L "$destination" ]]; then
        [[ -d "$destination" && ! -L "$destination" ]] ||
          die "file/directory conflict during migration: $relative_path"
      else
        mkdir -p "$destination"
      fi
      continue
    fi

    if [[ -f "$item" && ! -L "$item" ]]; then
      if [[ ! -e "$destination" && ! -L "$destination" ]]; then
        mkdir -p "$(dirname "$destination")"
        cp -a "$item" "$destination"
        continue
      fi
      [[ -f "$destination" && ! -L "$destination" ]] ||
        die "file type conflict during migration: $relative_path"
      cmp -s "$item" "$destination" && continue
      source_mtime="$(stat -c %Y "$item")"
      destination_mtime="$(stat -c %Y "$destination")"
      if ((source_mtime > destination_mtime)); then
        copy_variant "$destination" "$relative_path" "$destination_mtime"
        rm "$destination"
        cp -a "$item" "$destination"
      else
        copy_variant "$item" "$relative_path" "$source_mtime"
      fi
      continue
    fi

    if [[ -L "$item" ]]; then
      if [[ ! -e "$destination" && ! -L "$destination" ]]; then
        mkdir -p "$(dirname "$destination")"
        cp -a "$item" "$destination"
        continue
      fi
      [[ -L "$destination" ]] || die "symlink type conflict during migration: $relative_path"
      [[ "$(readlink "$item")" == "$(readlink "$destination")" ]] && continue
      source_mtime="$(stat -c %Y "$item")"
      destination_mtime="$(stat -c %Y "$destination")"
      if ((source_mtime > destination_mtime)); then
        copy_variant "$destination" "$relative_path" "$destination_mtime"
        rm "$destination"
        cp -a "$item" "$destination"
      else
        copy_variant "$item" "$relative_path" "$source_mtime"
      fi
      continue
    fi

    die "unsupported file type during migration: $relative_path"
  done < <(find "$local_root" -mindepth 1 -print0)
}

migrate_and_link() {
  local stamp source_parent backup local_backup
  stamp="$(date -u +%Y%m%dT%H%M%SZ)-$$"
  source_parent="$(dirname "$SOURCE")"
  STAGING="$source_parent/.lootbox-merge-$stamp"
  backup="$source_parent/.lootbox-backup-$stamp"
  local_backup="$TARGET/.lootbox-backup-$stamp"

  cleanup_staging() {
    if [[ -n "${STAGING:-}" && -d "$STAGING" ]]; then
      rm -rf -- "$STAGING"
    fi
  }

  if $DRY_RUN; then
    log "Would merge $LINK into $SOURCE"
    log "Would replace $LINK with an absolute symlink to $SOURCE"
    return 0
  fi

  mkdir "$STAGING"
  trap cleanup_staging EXIT
  rsync -a -- "$SOURCE/" "$STAGING/"
  merge_directory "$LINK"

  mv "$SOURCE" "$backup"
  if ! mv "$STAGING" "$SOURCE"; then
    mv "$backup" "$SOURCE"
    die "failed to install merged shared directory"
  fi

  if ! mv "$LINK" "$local_backup"; then
    mv "$SOURCE" "$STAGING"
    mv "$backup" "$SOURCE"
    cleanup_staging
    STAGING=""
    die "failed to preserve local directory; migration rolled back"
  fi
  if ! ln -s "$SOURCE" "$LINK"; then
    mv "$local_backup" "$LINK"
    mv "$SOURCE" "$STAGING"
    mv "$backup" "$SOURCE"
    rm -rf -- "$STAGING"
    STAGING=""
    die "failed to create symlink; migration rolled back"
  fi

  if [[ ! -L "$LINK" || "$(readlink "$LINK")" != "$SOURCE" ]]; then
    rm -f "$LINK"
    mv "$local_backup" "$LINK"
    mv "$SOURCE" "$STAGING"
    mv "$backup" "$SOURCE"
    rm -rf -- "$STAGING"
    STAGING=""
    die "symlink verification failed; migration rolled back"
  fi

  rm -rf -- "$local_backup" "$backup"
  STAGING=""
  trap - EXIT
  log "Migrated $LINK into $SOURCE"
  log "Linked $LINK -> $SOURCE"
}

if [[ -L "$LINK" ]]; then
  if [[ "$(readlink "$LINK")" == "$SOURCE" ]]; then
    log "Already linked: $LINK -> $SOURCE"
  elif [[ "$MODE" == "force" ]]; then
    if $DRY_RUN; then
      log "Would repoint $LINK -> $SOURCE"
    else
      old_target="$(readlink "$LINK")"
      unlink "$LINK"
      if ! create_link; then
        ln -s "$old_target" "$LINK"
        die "failed to repoint symlink; original restored"
      fi
    fi
  else
    die "$LINK is a different symlink; use --force to repoint it"
  fi
elif [[ -e "$LINK" ]]; then
  if [[ -d "$LINK" && "$MODE" == "migrate" ]]; then
    migrate_and_link
  elif [[ -d "$LINK" ]]; then
    die "$LINK is a real directory; use --migrate to preserve and merge it"
  else
    die "$LINK exists and is not a directory or symlink"
  fi
else
  if $DRY_RUN; then
    log "Would link $LINK -> $SOURCE"
  else
    create_link || die "failed to create symlink: $LINK"
  fi
fi

add_ignore_entry
