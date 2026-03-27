import * as vscode from "vscode";
import * as fs from "fs";
import * as path from "path";
import * as crypto from "crypto";
import * as os from "os";

const DEFAULT_BACKUP_DIR = path.join(
  process.env.HOME || process.env.USERPROFILE || "/tmp",
  ".vscode-buffer-backups"
);

const META_PREFIX = "// BUFFER-BACKUP-META: ";

interface Duration {
  years: number;
  months: number;
  days: number;
  hours: number;
  minutes: number;
  seconds: number;
}

function durationToSeconds(d: Duration): number {
  return (
    d.years * 365 * 86400 +
    d.months * 30 * 86400 +
    d.days * 86400 +
    d.hours * 3600 +
    d.minutes * 60 +
    d.seconds
  );
}

function getDuration(section: string): number {
  const c = vscode.workspace.getConfiguration(section);
  return durationToSeconds({
    years: c.get<number>("years", 0),
    months: c.get<number>("months", 0),
    days: c.get<number>("days", 0),
    hours: c.get<number>("hours", 0),
    minutes: c.get<number>("minutes", 0),
    seconds: c.get<number>("seconds", 0),
  });
}

function getInterval(key: string): number {
  const c = vscode.workspace.getConfiguration("bufferBackup");
  const v = c.get<number | null>(key, null);
  if (v === null || v === undefined || v === 0) {
    return 5000;
  }
  return Math.max(1, v) * 1000;
}

interface Config {
  backupDir: string;
  maxAgeSec: number;
  maxTotalMb: number;
  rolloutMb: number;
  maxFileCount: number;
  rolloutFileCount: number;
  debounceMs: number;
  ageCleanupMs: number;
  sizeCleanupMs: number;
  countCleanupMs: number;
}

function getConfig(): Config {
  const c = vscode.workspace.getConfiguration("bufferBackup");
  return {
    backupDir: c.get("backupDir", "") || DEFAULT_BACKUP_DIR,
    maxAgeSec: getDuration("bufferBackup.maxAge"),
    maxTotalMb: c.get("maxTotalMb", 0),
    rolloutMb: c.get("rolloutMb", 0),
    maxFileCount: c.get("maxFileCount", 0),
    rolloutFileCount: c.get("rolloutFileCount", 0),
    debounceMs: c.get("debounceMs", 3000),
    ageCleanupMs: getInterval("ageCleanupIntervalSeconds"),
    sizeCleanupMs: getInterval("sizeCleanupIntervalSeconds"),
    countCleanupMs: getInterval("countCleanupIntervalSeconds"),
  };
}

interface BackupEntry {
  name: string;
  full: string;
  mtimeMs: number;
  size: number;
}

function listBackups(dir: string): BackupEntry[] {
  if (!fs.existsSync(dir)) {
    return [];
  }
  const results: BackupEntry[] = [];
  let subs: string[];
  try {
    subs = fs.readdirSync(dir);
  } catch {
    return [];
  }
  for (const sub of subs) {
    const subFull = path.join(dir, sub);
    try {
      if (!fs.statSync(subFull).isDirectory()) {
        continue;
      }
    } catch {
      continue;
    }
    let files: string[];
    try {
      files = fs.readdirSync(subFull);
    } catch {
      continue;
    }
    for (const name of files) {
      const full = path.join(subFull, name);
      try {
        const st = fs.statSync(full);
        if (st.isFile()) {
          results.push({ name, full, mtimeMs: st.mtimeMs, size: st.size });
        }
      } catch {
        // skip files we can't stat
      }
    }
  }
  return results.sort((a, b) => a.mtimeMs - b.mtimeMs);
}

function rmEmpty(dir: string): void {
  try {
    if (
      fs.existsSync(dir) &&
      fs.statSync(dir).isDirectory() &&
      fs.readdirSync(dir).length === 0
    ) {
      fs.rmSync(dir, { recursive: false });
    }
  } catch {
    // best-effort cleanup
  }
}

function cleanupAge(cfg: Config): void {
  if (cfg.maxAgeSec <= 0) {
    return;
  }
  const cutoff = Date.now() - cfg.maxAgeSec * 1000;
  for (const e of listBackups(cfg.backupDir)) {
    if (e.mtimeMs < cutoff) {
      try {
        fs.unlinkSync(e.full);
      } catch {
        // skip
      }
      rmEmpty(path.dirname(e.full));
    }
  }
}

function cleanupSize(cfg: Config): void {
  if (cfg.maxTotalMb <= 0 || cfg.rolloutMb <= 0) {
    return;
  }
  const entries = listBackups(cfg.backupDir);
  let total = entries.reduce((s, e) => s + e.size, 0);
  const maxB = cfg.maxTotalMb * 1024 * 1024;
  const targetB = maxB - cfg.rolloutMb * 1024 * 1024;
  if (total <= maxB) {
    return;
  }
  for (const e of entries) {
    if (total <= targetB) {
      break;
    }
    total -= e.size;
    try {
      fs.unlinkSync(e.full);
    } catch {
      // skip
    }
    rmEmpty(path.dirname(e.full));
  }
}

function cleanupCount(cfg: Config): void {
  if (cfg.maxFileCount <= 0 || cfg.rolloutFileCount <= 0) {
    return;
  }
  const entries = listBackups(cfg.backupDir);
  if (entries.length <= cfg.maxFileCount) {
    return;
  }
  const n = Math.min(cfg.rolloutFileCount, entries.length);
  for (let i = 0; i < n; i++) {
    try {
      fs.unlinkSync(entries[i].full);
    } catch {
      // skip
    }
    rmEmpty(path.dirname(entries[i].full));
  }
}

function utcDayDir(base: string): string {
  const d = new Date();
  const day = d.toISOString().slice(0, 10);
  const dir = path.join(base, day);
  fs.mkdirSync(dir, { recursive: true });
  return dir;
}

function contentHash(s: string): string {
  return crypto.createHash("sha256").update(s).digest("hex");
}

const lastHash = new Map<string, string>();

type BackupTrigger = "change" | "focus" | "init";

interface BackupMeta {
  sha256: string;
  bufferUri: string;
  languageId: string;
  lineCount: number;
  charCount: number;
  firstLine: string;
  openTabs: number;
  untitledTabs: number;
  dirtyTabs: number;
  workspace: string;
  hostname: string;
  vscodeVersion: string;
  trigger: BackupTrigger;
  timestamp: string;
}

function getFirstLine(text: string): string {
  const lines = text.split("\n");
  for (const line of lines) {
    const trimmed = line.trim();
    if (trimmed.length > 0) {
      return trimmed.length > 120 ? trimmed.slice(0, 120) + "..." : trimmed;
    }
  }
  return "";
}

function getWorkspacePath(): string {
  const folders = vscode.workspace.workspaceFolders;
  if (!folders || folders.length === 0) {
    return "(no workspace)";
  }
  return folders.map((f) => f.uri.fsPath).join("; ");
}

function countTabs(): { open: number; untitled: number; dirty: number } {
  let open = 0;
  let untitled = 0;
  let dirty = 0;
  for (const group of vscode.window.tabGroups.all) {
    for (const tab of group.tabs) {
      open++;
      if (tab.isDirty) {
        dirty++;
      }
      const input = tab.input;
      if (input && typeof input === "object" && "uri" in input) {
        const uri = (input as { uri: vscode.Uri }).uri;
        if (uri.scheme === "untitled") {
          untitled++;
        }
      }
    }
  }
  return { open, untitled, dirty };
}

function buildMeta(
  doc: vscode.TextDocument,
  text: string,
  h: string,
  trigger: BackupTrigger
): BackupMeta {
  const tabs = countTabs();
  return {
    sha256: h,
    bufferUri: doc.uri.toString(),
    languageId: doc.languageId || "txt",
    lineCount: doc.lineCount,
    charCount: text.length,
    firstLine: getFirstLine(text),
    openTabs: tabs.open,
    untitledTabs: tabs.untitled,
    dirtyTabs: tabs.dirty,
    workspace: getWorkspacePath(),
    hostname: os.hostname(),
    vscodeVersion: vscode.version,
    trigger,
    timestamp: new Date().toISOString(),
  };
}

function formatMetaLine(meta: BackupMeta): string {
  return META_PREFIX + JSON.stringify(meta);
}

function backupDoc(
  doc: vscode.TextDocument,
  dir: string,
  trigger: BackupTrigger
): void {
  if (doc.uri.scheme !== "untitled") {
    return;
  }
  const text = doc.getText();
  if (text.length === 0) {
    return;
  }

  const key = doc.uri.toString();
  const h = contentHash(text);
  if (lastHash.get(key) === h) {
    return;
  }

  const name = doc.uri.path.replace(/[^a-zA-Z0-9]/g, "_") || "untitled";
  const ts = new Date().toISOString().replace(/[:.]/g, "-");
  const lang = doc.languageId || "txt";
  const dayDir = utcDayDir(dir);

  const meta = buildMeta(doc, text, h, trigger);
  const content = formatMetaLine(meta) + "\n" + text;

  fs.writeFileSync(path.join(dayDir, `${name}_${ts}.${lang}`), content);
  lastHash.set(key, h);
}

function backupAll(cfg: Config, trigger: BackupTrigger): void {
  for (const doc of vscode.workspace.textDocuments) {
    backupDoc(doc, cfg.backupDir, trigger);
  }
}

export function activate(ctx: vscode.ExtensionContext): void {
  let cfg = getConfig();
  fs.mkdirSync(cfg.backupDir, { recursive: true });

  const timers = new Map<string, ReturnType<typeof setTimeout>>();
  const cleanupTimers: ReturnType<typeof setInterval>[] = [];

  function setupCleanupTimers(): void {
    for (const t of cleanupTimers) {
      clearInterval(t);
    }
    cleanupTimers.length = 0;
    cleanupTimers.push(setInterval(() => cleanupAge(cfg), cfg.ageCleanupMs));
    cleanupTimers.push(setInterval(() => cleanupSize(cfg), cfg.sizeCleanupMs));
    cleanupTimers.push(
      setInterval(() => cleanupCount(cfg), cfg.countCleanupMs)
    );
  }

  setupCleanupTimers();

  ctx.subscriptions.push(
    vscode.workspace.onDidChangeTextDocument((e) => {
      const doc = e.document;
      if (doc.uri.scheme !== "untitled") {
        return;
      }
      const key = doc.uri.toString();
      const existing = timers.get(key);
      if (existing !== undefined) {
        clearTimeout(existing);
      }
      timers.set(
        key,
        setTimeout(() => backupDoc(doc, cfg.backupDir, "change"), cfg.debounceMs)
      );
    }),

    vscode.window.onDidChangeWindowState(() => {
      backupAll(cfg, "focus");
    }),

    vscode.workspace.onDidChangeConfiguration((e) => {
      if (e.affectsConfiguration("bufferBackup")) {
        cfg = getConfig();
        fs.mkdirSync(cfg.backupDir, { recursive: true });
        setupCleanupTimers();
      }
    }),

    {
      dispose: () => {
        for (const t of cleanupTimers) {
          clearInterval(t);
        }
        for (const t of timers.values()) {
          clearTimeout(t);
        }
      },
    }
  );

  // Run initial cleanup
  cleanupAge(cfg);
  cleanupSize(cfg);
  cleanupCount(cfg);

  // Initial backup of any already-open untitled buffers
  backupAll(cfg, "init");
}

export function deactivate(): void {
  // timers cleaned up via dispose subscription
}
