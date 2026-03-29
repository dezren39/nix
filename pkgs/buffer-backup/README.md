# Buffer Backup

**Automatically backs up every untitled/unsaved VS Code buffer to disk — so you never lose a scratch note, snippet, or draft again.**

Backups are written to `~/.vscode-buffer-backups/` organised by date, with SHA-256 deduplication, rich metadata headers, and configurable retention policies.

## Why

VS Code's built-in hot-exit helps, but it's not bulletproof. Crashes, forced quits, profile resets, or simply closing a tab and clicking "Don't Save" can vaporise hours of work in an untitled buffer. Buffer Backup writes every change to disk independently of VS Code's internal state, giving you a durable, browsable history of everything you've typed.

## Features

- **Automatic backup** of all `untitled` buffers — no manual action needed
- **SHA-256 dedup** — identical content is never written twice (except on close/save, which always capture a final snapshot)
- **Rich metadata** — every backup file includes a JSON header with buffer URI, language, line/char count, open tabs, terminals, workspace, hostname, trigger type, and more
- **Proper file extensions** — 90+ VS Code language IDs mapped to real extensions (`.md`, `.py`, `.ts`, …), not `.markdown` or `.plaintext`
- **Six backup triggers** covering every way a buffer can change or disappear
- **Three independent retention policies** — age-based, size-based, and count-based cleanup, each on its own configurable timer
- **Zero configuration required** — works out of the box with sensible defaults

## Backup Triggers

| Trigger | Fires when | Timing | Dedup |
|---|---|---|---|
| `change` | Text is edited | 3 s debounce (configurable) | SHA-256 skip if unchanged |
| `tab-switch` | You switch to a different tab | Immediate | SHA-256 skip if unchanged |
| `focus` | Window gains or loses focus | Immediate | SHA-256 skip if unchanged |
| `close` | Tab is closed | Immediate | **Always writes** (final snapshot) |
| `will-save` | Save dialog appears | Immediate | **Always writes** (final snapshot) |
| `init` | Extension activates on startup | Once | SHA-256 skip if unchanged |

## Backup Format

Each backup is a plain text file stored at:

```
~/.vscode-buffer-backups/YYYY-MM-DD/<buffer_name>_<ISO-timestamp><ext>
```

Example path:

```
~/.vscode-buffer-backups/2026-03-27/Untitled_2_2026-03-27T23-54-46-947Z.md
```

The **first line** of every file is a JSON metadata header:

```
// BUFFER-BACKUP-META: {"sha256":"abc123...","bufferUri":"untitled:Untitled-2","languageId":"markdown","fileExtension":".md","lineCount":42,"charCount":1847,"firstLine":"# My scratch notes","allTabs":["Untitled-2","extension.ts","README.md"],"openTabs":3,"untitledTabs":1,"dirtyTabs":2,"terminals":["zsh","node"],"workspace":"/Users/you/project","windowTitle":"project — VS Code","appName":"Visual Studio Code - Insiders","appHost":"desktop","hostname":"macbook.local","vscodeVersion":"1.99.0","previousBackupPath":"/path/to/previous/backup.md","trigger":"change","timestamp":"2026-03-27T23:54:46.947Z"}
```

The rest of the file is the buffer content, unmodified.

### Metadata Fields

| Field | Description |
|---|---|
| `sha256` | SHA-256 hash of the buffer content (excluding metadata line) |
| `bufferUri` | VS Code URI of the buffer (e.g. `untitled:Untitled-2`) |
| `languageId` | VS Code language identifier |
| `fileExtension` | Resolved file extension from language ID |
| `lineCount` | Number of lines in the buffer |
| `charCount` | Character count of the buffer |
| `firstLine` | First non-empty line (≤120 chars, preview) |
| `allTabs` | Labels of every open tab across all tab groups |
| `openTabs` | Total number of open tabs |
| `untitledTabs` | Number of untitled tabs |
| `dirtyTabs` | Number of unsaved/dirty tabs |
| `terminals` | Names of all open terminal instances |
| `workspace` | Workspace folder path(s) |
| `windowTitle` | Reconstructed window title |
| `appName` | VS Code application name |
| `appHost` | VS Code application host (e.g. `desktop`) |
| `hostname` | OS hostname |
| `vscodeVersion` | VS Code version string |
| `previousBackupPath` | Absolute path to this buffer's previous backup (`null` if first) |
| `trigger` | What caused this backup (`change`, `tab-switch`, `focus`, `close`, `will-save`, `init`) |
| `timestamp` | ISO 8601 timestamp |

## Configuration

All settings live under `bufferBackup.*` in VS Code settings. Everything is optional — the extension works with zero config.

### General

| Setting | Type | Default | Description |
|---|---|---|---|
| `bufferBackup.backupDir` | `string` | `~/.vscode-buffer-backups` | Backup directory path |
| `bufferBackup.debounceMs` | `integer` | `3000` | Debounce delay for text-change backups (ms) |

### Retention: Age

Delete backups older than a specified duration. Set any combination of time components.

| Setting | Type | Default | Description |
|---|---|---|---|
| `bufferBackup.maxAge.years` | `integer` | `0` | Years component |
| `bufferBackup.maxAge.months` | `integer` | `0` | Months component (30 days each) |
| `bufferBackup.maxAge.days` | `integer` | `0` | Days component |
| `bufferBackup.maxAge.hours` | `integer` | `0` | Hours component |
| `bufferBackup.maxAge.minutes` | `integer` | `0` | Minutes component |
| `bufferBackup.maxAge.seconds` | `integer` | `0` | Seconds component |

All zero (default) = age cleanup disabled.

### Retention: Size

Delete oldest backups when total size exceeds a threshold.

| Setting | Type | Default | Description |
|---|---|---|---|
| `bufferBackup.maxTotalMb` | `number` | `0` | Max total backup size in MB (0 = disabled) |
| `bufferBackup.rolloutMb` | `number` | `0` | MB to free when limit is hit (oldest first) |

Both must be set to enable size-based cleanup.

### Retention: Count

Delete oldest backups when file count exceeds a threshold.

| Setting | Type | Default | Description |
|---|---|---|---|
| `bufferBackup.maxFileCount` | `integer` | `0` | Max backup file count (0 = disabled) |
| `bufferBackup.rolloutFileCount` | `integer` | `0` | Files to delete when limit is hit (oldest first) |

Both must be set to enable count-based cleanup.

### Cleanup Intervals

Each retention policy runs on its own timer. By default all three run every 5 seconds.

| Setting | Type | Default | Description |
|---|---|---|---|
| `bufferBackup.ageCleanupIntervalSeconds` | `integer \| null` | `null` (5s) | Age cleanup interval in seconds |
| `bufferBackup.sizeCleanupIntervalSeconds` | `integer \| null` | `null` (5s) | Size cleanup interval in seconds |
| `bufferBackup.countCleanupIntervalSeconds` | `integer \| null` | `null` (5s) | Count cleanup interval in seconds |

Set to `null` or `0` for the default 5-second interval. Minimum is 1 second.

## Example Configuration

```jsonc
// settings.json
{
  // Keep backups for 30 days
  "bufferBackup.maxAge.days": 30,

  // Cap total size at 500 MB, free 50 MB at a time
  "bufferBackup.maxTotalMb": 500,
  "bufferBackup.rolloutMb": 50,

  // Keep at most 10,000 files, delete 100 at a time
  "bufferBackup.maxFileCount": 10000,
  "bufferBackup.rolloutFileCount": 100,

  // Faster change detection
  "bufferBackup.debounceMs": 1000
}
```

## Installation

### From source (symlink)

```bash
# Clone and build
cd pkgs/buffer-backup
npm install
npx tsc

# Symlink into VS Code Insiders
ln -s "$(pwd)" ~/.vscode-insiders/extensions/drewry-pope.buffer-backup-0.2.0

# Or for stable VS Code
ln -s "$(pwd)" ~/.vscode/extensions/drewry-pope.buffer-backup-0.2.0
```

Reload the window and the extension activates immediately.

### With Nix

```bash
# Build
nix build

# Development shell (bun + node + tsc)
nix develop

# Legacy nix-shell
nix-shell
```

## Supported Languages

The extension maps 90+ VS Code language identifiers to proper file extensions. Some highlights:

| Language ID | Extension |
|---|---|
| `markdown` | `.md` |
| `python` | `.py` |
| `typescript` | `.ts` |
| `typescriptreact` | `.tsx` |
| `javascript` | `.js` |
| `rust` | `.rs` |
| `go` | `.go` |
| `shellscript` | `.sh` |
| `nix` | `.nix` |
| `yaml` | `.yaml` |
| `json` | `.json` |
| `html` | `.html` |
| `css` | `.css` |
| `sql` | `.sql` |

Unmapped languages fall back to `.{languageId}`.

## How It Works

1. Extension activates on VS Code startup (`*` activation event)
2. Immediately backs up all currently open untitled buffers (`init` trigger)
3. Registers listeners for text changes, tab switches, focus changes, document close, and save events
4. On each trigger, computes SHA-256 of buffer content and skips if unchanged (except `close`/`will-save` which always write)
5. Writes `<metadata-line>\n<content>` to `~/.vscode-buffer-backups/YYYY-MM-DD/`
6. Three independent cleanup timers prune old, oversized, or excess backups on a configurable schedule

## License

MIT OR Apache-2.0
