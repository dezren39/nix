import { createHash } from "node:crypto";
import { basename, dirname, extname, join, relative } from "node:path";

const home = "/Users/drewry.pope";
const destination = join(home, ".config/nix/.lootbox");
const sourceRoots = [
  join(home, ".Trash/.lootbox"),
  join(home, ".Trash/.opencode/worktrees/lift-shift/.lootbox"),
  join(home, ".config/lootbox"),
  join(home, ".local/share/lootbox"),
  join(home, "Library/Application Support/lootbox"),
  join(
    home,
    "Library/Caches/deno/gen/file/private/var/folders/k9/k2ksrtk174g4lmbcnfvn4xv40000gp/T/opencode/lootbox-smoke/.lootbox",
  ),
  join(
    home,
    "Library/Caches/deno/gen/file/private/var/folders/k9/k2ksrtk174g4lmbcnfvn4xv40000gp/T/opencode/lootbox-upstream/.lootbox",
  ),
  join(
    home,
    "Library/CloudStorage/OneDrive-Vertex,Inc/Documents/plans/history/lootbox",
  ),
  join(
    home,
    "Library/Group Containers/UBF8T346G9.OneDriveStandaloneSuite/OneDrive - Vertex, Inc.noindex/OneDrive - Vertex, Inc/Documents/plans/history/lootbox",
  ),
  join(home, "git/.trash/.opencode/worktrees/cleanup/.lootbox"),
  join(home, "git/.trash/incident-response-management-up2/.lootbox"),
  join(home, "git/.trash/incident-response-management-up2/plans/history/lootbox"),
  join(home, "git/incident-response-management-trunk/.lootbox"),
  join(home, "git/lootbox"),
  join(home, "git/lootbox/.lootbox"),
  join(home, "git/operations-portal/.lootbox"),
  join(home, "git/operations-portal/.worktrees/feat-vod-config-edit/.lootbox"),
];

type Candidate = {
  path: string;
  source: string;
  mtime: Date;
  mode: number | null;
  size: number;
  hash?: string;
};

const files = new Map<string, Candidate[]>();
const directories = new Set<string>([""]);
const skippedSymlinks: string[] = [];
const missingRoots: string[] = [];

async function collect(root: string, includeRoot: boolean) {
  try {
    const stat = await Deno.lstat(root);
    if (!stat.isDirectory || stat.isSymlink) {
      missingRoots.push(root);
      return;
    }
  } catch (error) {
    if (error instanceof Deno.errors.NotFound) {
      missingRoots.push(root);
      return;
    }
    throw error;
  }

  async function walk(path: string) {
    for await (const entry of Deno.readDir(path)) {
      const absolute = join(path, entry.name);
      const relativePath = relative(root, absolute);
      if (entry.isSymlink) {
        skippedSymlinks.push(absolute);
      } else if (entry.isDirectory) {
        directories.add(relativePath);
        await walk(absolute);
      } else if (entry.isFile) {
        const stat = await Deno.stat(absolute);
        const candidates = files.get(relativePath) ?? [];
        candidates.push({
          path: absolute,
          source: root,
          mtime: stat.mtime ?? new Date(0),
          mode: stat.mode,
          size: stat.size,
        });
        files.set(relativePath, candidates);
      }
    }
  }

  if (includeRoot) await walk(root);
}

async function sha256(candidate: Candidate) {
  if (candidate.hash) return candidate.hash;
  const hash = createHash("sha256");
  const file = await Deno.open(candidate.path, { read: true });
  try {
    for await (const chunk of file.readable) hash.update(chunk);
  } finally {
    try {
      file.close();
    } catch {
      // Consuming the stream normally closes the file.
    }
  }
  candidate.hash = hash.digest("hex");
  return candidate.hash;
}

function compareNewest(a: Candidate, b: Candidate) {
  return b.mtime.getTime() - a.mtime.getTime() || a.path.localeCompare(b.path);
}

function datedName(path: string, date: Date, sequence?: number) {
  const extension = extname(path);
  const stem = basename(path, extension);
  const day = date.toISOString().slice(0, 10);
  const counter = sequence ? `_${sequence}` : "";
  return join(dirname(path), `${stem}_${day}${counter}${extension}`);
}

async function copy(candidate: Candidate, target: string) {
  await Deno.mkdir(dirname(target), { recursive: true });
  await Deno.copyFile(candidate.path, target);
  if (candidate.mode !== null) await Deno.chmod(target, candidate.mode & 0o7777);
  await Deno.utime(target, candidate.mtime, candidate.mtime);
}

await collect(destination, true);
for (const root of sourceRoots) await collect(root, true);

for (const filePath of files.keys()) {
  let parent = dirname(filePath);
  while (parent !== "." && parent !== "") {
    if (files.has(parent)) {
      throw new Error(`Cannot merge file/directory conflict: ${parent}`);
    }
    parent = dirname(parent);
  }
  if (directories.has(filePath)) {
    throw new Error(`Cannot merge directory/file conflict: ${filePath}`);
  }
}

const reserved = new Set(files.keys());
const planned = new Map<string, Candidate>();
let duplicateCopies = 0;
let conflicts = 0;

for (const [relativePath, candidates] of [...files.entries()].sort(([a], [b]) =>
  a.localeCompare(b)
)) {
  const unique = new Map<string, Candidate>();
  for (const candidate of candidates.sort(compareNewest)) {
    const hash = await sha256(candidate);
    if (unique.has(hash)) {
      duplicateCopies++;
    } else {
      unique.set(hash, candidate);
    }
  }

  const versions = [...unique.values()].sort(compareNewest);
  planned.set(relativePath, versions[0]);
  if (versions.length === 1) continue;
  conflicts++;

  for (const candidate of versions.slice(1)) {
    let sequence: number | undefined;
    let variant = datedName(relativePath, candidate.mtime);
    while (reserved.has(variant)) {
      sequence = (sequence ?? 1) + 1;
      variant = datedName(relativePath, candidate.mtime, sequence);
    }
    reserved.add(variant);
    planned.set(variant, candidate);
  }
}

const parent = dirname(destination);
const stamp = new Date().toISOString().replaceAll(/[:.]/g, "-");
const staging = join(parent, `.lootbox-merge-${stamp}`);
const backup = join(parent, `.lootbox-backup-${stamp}`);

await Deno.mkdir(staging, { recursive: false });
try {
  for (const directory of [...directories].sort()) {
    if (directory) await Deno.mkdir(join(staging, directory), { recursive: true });
  }
  for (const [relativePath, candidate] of planned) {
    await copy(candidate, join(staging, relativePath));
  }

  await Deno.rename(destination, backup);
  try {
    await Deno.rename(staging, destination);
  } catch (error) {
    await Deno.rename(backup, destination);
    throw error;
  }
  await Deno.remove(backup, { recursive: true });
} catch (error) {
  try {
    await Deno.remove(staging, { recursive: true });
  } catch {
    // Preserve the original error.
  }
  throw error;
}

const bytes = [...planned.values()].reduce((sum, candidate) => sum + candidate.size, 0);
console.log(JSON.stringify({
  destination,
  sourceRoots: sourceRoots.length,
  filesWritten: planned.size,
  bytesWritten: bytes,
  conflicts,
  duplicateCopiesSkipped: duplicateCopies,
  symlinksSkipped: skippedSymlinks.length,
  missingRoots,
}, null, 2));
