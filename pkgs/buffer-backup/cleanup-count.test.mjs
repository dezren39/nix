// Fixture test for cleanupCount in src/extension.ts (#1002).
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

console.log("cleanup-count.test.mjs: all assertions passed");
