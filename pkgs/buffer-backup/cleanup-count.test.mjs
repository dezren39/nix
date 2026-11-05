// Fixture test for cleanupCount in src/extension.ts (#1002, #1014, #1211,
// #1375).
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
   ${stripTypes(extractFunction("cleanupAge"))}
   return { listBackups, cleanupSize, cleanupCount, cleanupAge };`,
);
const { listBackups, cleanupSize, cleanupCount, cleanupAge } = factory(fs, path);

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

// 7. rollout == max, cleanupCount (#1211): the raw target is 0 and
//    the unguarded pass deleted all 20 files. The floored target
//    leaves exactly the newest backup.
{
  const dir = fixture(20);
  cleanupCount({ backupDir: dir, maxFileCount: 10, rolloutFileCount: 10 });
  assert.deepStrictEqual(remainingNames(dir), ["f19.txt"]);
  fs.rmSync(dir, { recursive: true, force: true });
  console.log("pass: cleanupCount with rollout == max leaves the newest backup (#1211)");
}

// 8. rollout > max, cleanupCount (#1211): same floor applies.
{
  const dir = fixture(20);
  cleanupCount({ backupDir: dir, maxFileCount: 10, rolloutFileCount: 25 });
  assert.deepStrictEqual(remainingNames(dir), ["f19.txt"]);
  fs.rmSync(dir, { recursive: true, force: true });
  console.log("pass: cleanupCount with rollout > max leaves the newest backup (#1211)");
}

// 9. rollout == max, cleanupSize (#1211): the raw target is 0 bytes;
//    the floored target (newest file's size) leaves exactly it.
{
  const dir = fixture(20, 1024);
  cleanupSize({ backupDir: dir, maxTotalMb: 0.01, rolloutMb: 0.01 });
  const entries = listBackups(dir);
  assert.strictEqual(entries.length, 1);
  assert.strictEqual(entries[0].name, "f19.txt", "newest file must survive");
  fs.rmSync(dir, { recursive: true, force: true });
  console.log("pass: cleanupSize with rollout == max leaves the newest backup (#1211)");
}

// 10. rollout > max, cleanupSize (#1211): the raw target is
//     negative; same floor applies.
{
  const dir = fixture(20, 1024);
  cleanupSize({ backupDir: dir, maxTotalMb: 0.01, rolloutMb: 0.05 });
  const entries = listBackups(dir);
  assert.strictEqual(entries.length, 1);
  assert.strictEqual(entries[0].name, "f19.txt", "newest file must survive");
  fs.rmSync(dir, { recursive: true, force: true });
  console.log("pass: cleanupSize with rollout > max leaves the newest backup (#1211)");
}

// 14. Failed unlink must not cost the newest backup, cleanupCount
//     (#1530): four 16-byte backups, the oldest in a read-only day dir
//     so its unlink fails. maxFileCount 2, rollout 1 -> target 1. The
//     #1014 accounting (a failed unlink is not a deletion) makes an
//     unguarded loop walk one entry further per failure and delete
//     f03, the newest — the pass ended at target with only the
//     undeletable oldest left. The structural newest exclusion
//     (slice(0, -1), as cleanupAge has always had) leaves f00 (failed
//     unlink) and f03 (newest) standing.
function fixtureUndeletableOldestFlat() {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "buffer-backup-test-"));
  const base = Date.UTC(2026, 0, 1) / 1000;
  const oldDay = path.join(dir, "2026-01-01");
  fs.mkdirSync(oldDay);
  const f0 = path.join(oldDay, "f00.txt");
  fs.writeFileSync(f0, "x".repeat(100));
  fs.utimesSync(f0, base, base);
  const day = path.join(dir, "2026-01-02");
  fs.mkdirSync(day);
  for (let i = 1; i < 4; i++) {
    const full = path.join(day, `f0${i}.txt`);
    fs.writeFileSync(full, "x".repeat(100));
    fs.utimesSync(full, base + i * 10, base + i * 10);
  }
  fs.chmodSync(oldDay, 0o555);
  return { dir, oldDay };
}

{
  const { dir, oldDay } = fixtureUndeletableOldestFlat();
  cleanupCount({ backupDir: dir, maxFileCount: 2, rolloutFileCount: 1 });
  assert.deepStrictEqual(remainingNames(dir), ["f00.txt", "f03.txt"]);
  removeFixture(dir, oldDay);
  console.log("pass: cleanupCount failed unlink does not delete the newest backup (#1530)");
}

// 15. Failed unlink must not cost the newest backup, cleanupSize
//     (#1530): same fixture (4x100B) with a 250-byte cap and 100-byte
//     rollout -> target 150B. The unguarded loop deleted f01, f02 and
//     then the newest f03 to get under target; the guarded pass stops
//     at the slice end with f00 (failed unlink) and f03 (newest)
//     standing, still over target — over target with the newest
//     intact is the correct outcome.
{
  const { dir, oldDay } = fixtureUndeletableOldestFlat();
  cleanupSize({ backupDir: dir, maxTotalMb: 250 / (1024 * 1024), rolloutMb: 100 / (1024 * 1024) });
  assert.deepStrictEqual(remainingNames(dir), ["f00.txt", "f03.txt"]);
  removeFixture(dir, oldDay);
  console.log("pass: cleanupSize failed unlink does not delete the newest backup (#1530)");
}

// 11. All backups expired, cleanupAge (#1375): the fixture mtimes are
//     2026-01-01, so with maxAgeSec = 3600 every file is past the
//     cutoff. The unguarded pass deleted all of them; the #1211-style
//     floor leaves exactly the newest backup.
{
  const dir = fixture(5);
  cleanupAge({ backupDir: dir, maxAgeSec: 3600 });
  assert.deepStrictEqual(remainingNames(dir), ["f04.txt"]);
  fs.rmSync(dir, { recursive: true, force: true });
  console.log("pass: cleanupAge with every backup expired leaves the newest backup (#1375)");
}

// 12. A single expired backup, cleanupAge (#1375): the newest-backup
//     floor also protects a store with exactly one entry.
{
  const dir = fixture(1);
  cleanupAge({ backupDir: dir, maxAgeSec: 3600 });
  assert.deepStrictEqual(remainingNames(dir), ["f00.txt"]);
  fs.rmSync(dir, { recursive: true, force: true });
  console.log("pass: cleanupAge leaves a lone expired backup standing (#1375)");
}

// 13. Mixed ages, cleanupAge (#1375): mtimes relative to now -- two
//     expired (3h, 2h old) and one fresh (1 minute old), maxAgeSec
//     3600. Only the expired non-newest entries are deleted.
{
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "buffer-backup-test-"));
  const day = path.join(dir, "2026-01-01");
  fs.mkdirSync(day);
  const nowSec = Date.now() / 1000;
  const ages = [["old1.txt", 3 * 3600], ["old2.txt", 2 * 3600], ["fresh.txt", 60]];
  for (const [name, age] of ages) {
    const full = path.join(day, name);
    fs.writeFileSync(full, "x".repeat(16));
    fs.utimesSync(full, nowSec - age, nowSec - age);
  }
  cleanupAge({ backupDir: dir, maxAgeSec: 3600 });
  assert.deepStrictEqual(remainingNames(dir), ["fresh.txt"]);
  fs.rmSync(dir, { recursive: true, force: true });
  console.log("pass: cleanupAge deletes only the expired non-newest backups (#1375)");
}

// 14. Symlinked day-directory (#1524): a symlink inside the backup dir
//     pointing at a sibling directory outside it. listBackups must not
//     descend into it, and none of the three cleanup passes may delete
//     the outside files (they are old enough / numerous enough that
//     every pass would take them if they were listed).
function fixtureSymlinkedDay() {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "buffer-backup-link-"));
  const dir = path.join(root, "backups");
  const outside = path.join(root, "outside");
  const day = path.join(dir, "2026-01-01");
  fs.mkdirSync(day, { recursive: true });
  fs.mkdirSync(outside);
  const base = Date.UTC(2026, 0, 1) / 1000;
  for (let i = 0; i < 3; i++) {
    const full = path.join(day, `real${i}.txt`);
    fs.writeFileSync(full, "x".repeat(1024));
    fs.utimesSync(full, base + i * 10, base + i * 10);
  }
  const outsideFiles = ["victim1.txt", "victim2.txt"].map((name, i) => {
    const full = path.join(outside, name);
    fs.writeFileSync(full, "precious");
    fs.utimesSync(full, base + i * 10, base + i * 10);
    return full;
  });
  fs.symlinkSync(outside, path.join(dir, "2020-01-01"), "dir");
  return { root, dir, outsideFiles };
}

{
  const { root, dir, outsideFiles } = fixtureSymlinkedDay();
  assert.deepStrictEqual(remainingNames(dir), ["real0.txt", "real1.txt", "real2.txt"]);
  cleanupAge({ backupDir: dir, maxAgeSec: 3600 });
  for (const f of outsideFiles) assert.ok(fs.existsSync(f), `cleanupAge deleted ${f}`);
  fs.rmSync(root, { recursive: true, force: true });
  console.log("pass: cleanupAge never follows a symlinked day-directory (#1524)");
}
{
  const { root, dir, outsideFiles } = fixtureSymlinkedDay();
  cleanupSize({ backupDir: dir, maxTotalMb: 0.001, rolloutMb: 0.0005 });
  for (const f of outsideFiles) assert.ok(fs.existsSync(f), `cleanupSize deleted ${f}`);
  assert.ok(fs.lstatSync(path.join(dir, "2020-01-01")).isSymbolicLink(), "symlink itself must remain");
  fs.rmSync(root, { recursive: true, force: true });
  console.log("pass: cleanupSize never follows a symlinked day-directory (#1524)");
}
{
  const { root, dir, outsideFiles } = fixtureSymlinkedDay();
  cleanupCount({ backupDir: dir, maxFileCount: 2, rolloutFileCount: 1 });
  for (const f of outsideFiles) assert.ok(fs.existsSync(f), `cleanupCount deleted ${f}`);
  assert.deepStrictEqual(remainingNames(dir), ["real2.txt"]);
  fs.rmSync(root, { recursive: true, force: true });
  console.log("pass: cleanupCount never follows a symlinked day-directory (#1524)");
}

// 15. Symlinked file inside a real day directory (#1524): the link is
//     not a backup entry and its outside target is never deleted.
{
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "buffer-backup-link-"));
  const dir = path.join(root, "backups");
  const day = path.join(dir, "2026-01-01");
  fs.mkdirSync(day, { recursive: true });
  const target = path.join(root, "target.txt");
  fs.writeFileSync(target, "precious");
  const base = Date.UTC(2026, 0, 1) / 1000;
  fs.utimesSync(target, base, base);
  fs.symlinkSync(target, path.join(day, "linked.txt"));
  const real = path.join(day, "real.txt");
  fs.writeFileSync(real, "x".repeat(16));
  fs.utimesSync(real, base + 10, base + 10);
  assert.deepStrictEqual(remainingNames(dir), ["real.txt"]);
  cleanupAge({ backupDir: dir, maxAgeSec: 3600 });
  assert.ok(fs.existsSync(target), "cleanupAge deleted the symlink target");
  fs.rmSync(root, { recursive: true, force: true });
  console.log("pass: symlinked files are not backup entries (#1524)");
}

console.log("cleanup-count.test.mjs: all assertions passed");
