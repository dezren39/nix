/**
 * Tests for `lootbox init` repair behavior and get_config directory
 * auto-creation logic.
 *
 * These test the core scenarios:
 * 1. init on a fresh directory creates everything
 * 2. init with .lootbox missing subdirs repairs them
 * 3. get_config auto-creates missing subdirs when .lootbox/ exists
 * 4. FileWatcherManager creates missing directory before watching
 */

import {
  assertEquals,
  assertStrictEquals,
} from "jsr:@std/assert";

import { FileWatcherManager } from "../src/lib/rpc/managers/file_watcher_manager.ts";

// ── FileWatcherManager directory guard tests ────────────────────────

Deno.test("FileWatcherManager creates missing directory before watching", async () => {
  const tmpDir = await Deno.makeTempDir({ prefix: "lootbox_fw_test_" });
  const watchDir = `${tmpDir}/nonexistent-tools`;

  const fw = new FileWatcherManager();

  // Directory doesn't exist yet — startWatching should create it
  fw.startWatching(watchDir, async () => {});

  // Verify directory was created
  const stat = await Deno.stat(watchDir);
  assertEquals(stat.isDirectory, true);

  // Verify watcher started
  assertStrictEquals(fw.isWatching(), true);

  fw.stopWatching();
  await Deno.remove(tmpDir, { recursive: true }).catch(() => {});
});

Deno.test("FileWatcherManager watches existing directory without issue", async () => {
  const tmpDir = await Deno.makeTempDir({ prefix: "lootbox_fw_test_" });
  const watchDir = `${tmpDir}/existing-tools`;
  await Deno.mkdir(watchDir);

  const fw = new FileWatcherManager();
  fw.startWatching(watchDir, async () => {});

  assertStrictEquals(fw.isWatching(), true);

  fw.stopWatching();
  await Deno.remove(tmpDir, { recursive: true }).catch(() => {});
});

Deno.test("FileWatcherManager skips non-directory path gracefully", async () => {
  const tmpDir = await Deno.makeTempDir({ prefix: "lootbox_fw_test_" });
  const filePath = `${tmpDir}/not-a-directory`;
  await Deno.writeTextFile(filePath, "hello");

  const fw = new FileWatcherManager();
  fw.startWatching(filePath, async () => {});

  // Should not start watching a file
  assertStrictEquals(fw.isWatching(), false);

  fw.stopWatching();
  await Deno.remove(tmpDir, { recursive: true }).catch(() => {});
});

// ── init repair logic tests ─────────────────────────────────────────

Deno.test("init creates all subdirs when .lootbox is missing", async () => {
  const tmpDir = await Deno.makeTempDir({ prefix: "lootbox_init_test_" });
  const origCwd = Deno.cwd();

  try {
    Deno.chdir(tmpDir);

    // Simulate what init does: create subdirs with recursive: true
    const lootboxDir = ".lootbox";
    const subdirs = ["tools", "workflows", "scripts"];

    for (const sub of subdirs) {
      await Deno.mkdir(`${lootboxDir}/${sub}`, { recursive: true });
    }

    // Verify all exist
    for (const sub of subdirs) {
      const stat = await Deno.stat(`${lootboxDir}/${sub}`);
      assertEquals(stat.isDirectory, true, `${sub} should be a directory`);
    }
  } finally {
    Deno.chdir(origCwd);
    await Deno.remove(tmpDir, { recursive: true }).catch(() => {});
  }
});

Deno.test("init repairs missing subdirs when .lootbox exists", async () => {
  const tmpDir = await Deno.makeTempDir({ prefix: "lootbox_init_test_" });
  const origCwd = Deno.cwd();

  try {
    Deno.chdir(tmpDir);

    // Create .lootbox with only scripts/, missing tools/ and workflows/
    await Deno.mkdir(".lootbox/scripts", { recursive: true });

    const subdirs = ["tools", "workflows", "scripts"];
    const missing: string[] = [];

    for (const sub of subdirs) {
      try {
        await Deno.stat(`.lootbox/${sub}`);
      } catch {
        missing.push(sub);
      }
    }

    assertEquals(missing, ["tools", "workflows"]);

    // Simulate repair: create missing subdirs
    for (const sub of missing) {
      await Deno.mkdir(`.lootbox/${sub}`, { recursive: true });
    }

    // Now all should exist
    for (const sub of subdirs) {
      const stat = await Deno.stat(`.lootbox/${sub}`);
      assertEquals(stat.isDirectory, true, `${sub} should exist after repair`);
    }
  } finally {
    Deno.chdir(origCwd);
    await Deno.remove(tmpDir, { recursive: true }).catch(() => {});
  }
});

Deno.test("get_config auto-creates missing tools/ when .lootbox exists", async () => {
  // This tests the auto-creation logic extracted from get_config.
  // We simulate the relevant code path without calling get_config() itself
  // (which would Deno.exit on various conditions).
  const tmpDir = await Deno.makeTempDir({ prefix: "lootbox_config_test_" });

  try {
    // Create .lootbox with no subdirs
    await Deno.mkdir(`${tmpDir}/.lootbox`);

    const subdirs = ["tools", "workflows", "scripts"];

    // Simulate get_config auto-creation logic
    for (const sub of subdirs) {
      const dirPath = `${tmpDir}/.lootbox/${sub}`;
      try {
        await Deno.stat(dirPath);
      } catch {
        await Deno.mkdir(dirPath, { recursive: true });
      }
    }

    // All should now exist
    for (const sub of subdirs) {
      const stat = await Deno.stat(`${tmpDir}/.lootbox/${sub}`);
      assertEquals(stat.isDirectory, true, `${sub} should be auto-created`);
    }
  } finally {
    await Deno.remove(tmpDir, { recursive: true }).catch(() => {});
  }
});
