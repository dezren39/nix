// Fixture test for cleanupCount in src/extension.ts (#1002, #1014).
//
// Run: node pkgs/buffer-backup/cleanup-count.test.mjs
//
// cleanupCount / cleanupSize / listBackups are not exported (extension.ts
// imports vscode), so — like the probe in #1002 — this test extracts those
// functions verbatim from extension.ts, strips the handful of TS type
// annotations they use, and drives the real code against a fixture
// directory of files with staggered mtimes.
//
// Post-condition under test: after ONE cleanupCount pass over a directory
// far over maxFileCount, at most maxFileCount files remain, the oldest
// are deleted first, and cleanupSize behaviour is unchanged.
import * as fs from "fs";
import * as path from "path";
import * as os from "os";
import assert from "node:assert";
import { fileURLToPath } from "url";

const here = path.dirname(fileURLToPath(import.meta.url));
const src = fs.readFileSync(path.join(here, "src", "extension.ts"), "utf8");

function extractFunction(name) {
  const start = src.indexOf(`function ${name}(`);
  assert.notStrictEqual(start, -1, `function ${name} not found in extension.ts`);
  const open = src.indexOf("{", start);
  let depth = 0;
  for (let i = open; i < src.length; i++) {
    if (src[i] === "{") depth++;
    else if (src[i] === "}") {
      depth--;
      if (depth === 0) return src.slice(start, i + 1);
    }
  }
  throw new Error(`unterminated function ${name}`);
}

function stripTypes(code) {
  return code
    .replace(/: BackupEntry\[\]/g, "")
    .replace(/: string\[\]/g, "")
    .replace(/: string/g, "")
    .replace(/: boolean/g, "")
    .replace(/: number/g, "")
    .replace(/: void/g, "")
    .replace(/: Config/g, "");
}

const factory = new Function(
  "fs",
  "path",
  `${stripTypes(extractFunction("listBackups"))}
   ${stripTypes(extractFunction("rmEmpty"))}
   ${stripTypes(extractFunction("cleanupSize"))}
   ${stripTypes(extractFunction("cleanupCount"))}
   return { listBackups, cleanupSize, cleanupCount };`,
);
const { listBackups, cleanupSize, cleanupCount } = factory(fs, path);

function fixture(count, sizeBytes = 16) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "buffer-backup-test-"));
  const day = path.join(dir, "2026-01-01");
  fs.mkdirSync(day);
  const base = Date.UTC(2026, 0, 1) / 1000;
  for (let i = 0; i < count; i++) {
    const name = `f${String(i).padStart(2, "0")}.txt`;
    const full = path.join(day, name);
    fs.writeFileSync(full, "x".repeat(sizeBytes));
    fs.utimesSync(full, base + i * 10, base + i * 10);
  }
  return dir;
}

function remainingNames(dir) {
  return listBackups(dir).map((e) => e.name);
}

// 1. Far over the limit: 30 files, max 10, rollout 5 -> one pass must
//    restore the limit (mirroring cleanupSize headroom: max - rollout = 5
//    remain), deleting the oldest first so the newest survive.
{
  const dir = fixture(30);
  cleanupCount({ backupDir: dir, maxFileCount: 10, rolloutFileCount: 5 });
  const names = remainingNames(dir);
  assert.ok(names.length <= 10, `expected <=10 files after one pass, got ${names.length}`);
  assert.deepStrictEqual(names, ["f25.txt", "f26.txt", "f27.txt", "f28.txt", "f29.txt"]);
  fs.rmSync(dir, { recursive: true, force: true });
  console.log("pass: over-limit directory converges in one cleanupCount pass, oldest deleted first");
}

// 2. At the limit: nothing is deleted.
{
  const dir = fixture(10);
  cleanupCount({ backupDir: dir, maxFileCount: 10, rolloutFileCount: 5 });
  assert.strictEqual(remainingNames(dir).length, 10);
  fs.rmSync(dir, { recursive: true, force: true });
  console.log("pass: at-limit directory is untouched");
}

// 3. Just over the limit: still lands at or below maxFileCount in one pass.
{
  const dir = fixture(11);
  cleanupCount({ backupDir: dir, maxFileCount: 10, rolloutFileCount: 5 });
  const names = remainingNames(dir);
  assert.ok(names.length <= 10, `expected <=10 files, got ${names.length}`);
  assert.strictEqual(names[names.length - 1], "f10.txt", "newest file must survive");
  fs.rmSync(dir, { recursive: true, force: true });
  console.log("pass: just-over-limit directory converges in one pass");
}

// 4. Control: cleanupSize behaviour is unchanged (30x1024B, max 0.02MB,
//    rollout 0.01MB -> 10 files / 10240B remain after one pass).
{
  const dir = fixture(30, 1024);
  cleanupSize({ backupDir: dir, maxTotalMb: 0.02, rolloutMb: 0.01 });
  const entries = listBackups(dir);
  assert.strictEqual(entries.length, 10);
  assert.strictEqual(entries.reduce((s, e) => s + e.size, 0), 10240);
  fs.rmSync(dir, { recursive: true, force: true });
  console.log("pass: cleanupSize control unchanged");
}

// 5. Failed unlink, cleanupCount (#1014): the oldest file sits in a
//    read-only day directory, so unlinking it fails. It must NOT be
//    counted as deleted: the pass keeps deleting newer files until
//    the target (max - rollout = 5) is actually reached, leaving the
//    undeletable oldest plus the 4 newest deletable files. The buggy
//    accounting stopped one file early, leaving 6 files.
//    (Assumes a non-root test user: root bypasses directory write
//    permissions and could unlink from a read-only directory.)
function fixtureUndeletableOldest(count, sizeBytes = 16) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "buffer-backup-test-"));
  const base = Date.UTC(2026, 0, 1) / 1000;
  const oldDay = path.join(dir, "2026-01-01");
  fs.mkdirSync(oldDay);
  const oldest = path.join(oldDay, "f00.txt");
  fs.writeFileSync(oldest, "x".repeat(sizeBytes));
  fs.utimesSync(oldest, base, base);
  const day = path.join(dir, "2026-01-02");
  fs.mkdirSync(day);
  for (let i = 1; i < count; i++) {
    const name = `f${String(i).padStart(2, "0")}.txt`;
    const full = path.join(day, name);
    fs.writeFileSync(full, "x".repeat(sizeBytes));
    fs.utimesSync(full, base + i * 10, base + i * 10);
  }
  fs.chmodSync(oldDay, 0o555);
  return { dir, oldDay };
}

function removeFixture(dir, oldDay) {
  try { fs.chmodSync(oldDay, 0o755); } catch {}
  fs.rmSync(dir, { recursive: true, force: true });
}

{
  const { dir, oldDay } = fixtureUndeletableOldest(30);
  cleanupCount({ backupDir: dir, maxFileCount: 10, rolloutFileCount: 5 });
  const names = remainingNames(dir);
  assert.deepStrictEqual(names, ["f00.txt", "f26.txt", "f27.txt", "f28.txt", "f29.txt"]);
  removeFixture(dir, oldDay);
  console.log("pass: cleanupCount does not count a failed unlink as deleted (#1014)");
}

// 6. Failed unlink, cleanupSize (#1014): same read-only-oldest fixture
//    at 30x1024B with the scenario-4 limits (max 0.02MB, rollout
//    0.01MB). The pass must keep
//    deleting until the accounted total reaches the target, leaving
//    the undeletable oldest plus the 9 newest files (10 files /
//    10240B on disk). The buggy accounting ended the pass one file
//    early, still over the size target.
{
  const { dir, oldDay } = fixtureUndeletableOldest(30, 1024);
  cleanupSize({ backupDir: dir, maxTotalMb: 0.02, rolloutMb: 0.01 });
  const entries = listBackups(dir);
  assert.strictEqual(entries.length, 10);
  assert.strictEqual(entries[0].name, "f00.txt", "undeletable oldest must survive");
  assert.strictEqual(entries.reduce((s, e) => s + e.size, 0), 10240);
  removeFixture(dir, oldDay);
  console.log("pass: cleanupSize does not count a failed unlink as deleted (#1014)");
}

console.log("cleanup-count.test.mjs: all assertions passed");
