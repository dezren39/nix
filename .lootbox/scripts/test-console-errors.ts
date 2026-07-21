// @ts-nocheck
// deno-lint-ignore-file no-explicit-any
/**
 * test-console-errors.ts — Phase 19.7.A.4: Check Chrome console for warnings/errors on each page
 * @example lootbox test-console-errors.ts
 */
const cd = tools.mcp_chrome_devtools;

function text(r: any): string {
  return r.content.map((c: any) => c.text ?? "").join("");
}

async function evalJs(fn: string): Promise<any> {
  const r = await cd.evaluate_script({ function: fn });
  const raw = text(r);
  const m = raw.match(/```(?:json)?\n([\s\S]*?)\n```/);
  try {
    return JSON.parse(m ? m[1] : raw);
  } catch {
    return m ? m[1] : raw;
  }
}

async function wait(ms: number): Promise<void> {
  await evalJs(
    `async () => { await new Promise(r => setTimeout(r, ${ms})); return "waited ${ms}ms"; }`,
  );
}

const SCREENSHOT_DIR =
  "/Users/drewry.pope/.config/nix/.opencode/worktrees/theme-align/features/2026-04-06_0019.0_theme-alignment-bootstrap-removal/screenshots";

const PAGES = [
  { url: "http://localhost:8080/", name: "home" },
  { url: "http://localhost:8080/irm/incidents", name: "incidents" },
  { url: "http://localhost:8080/irm/teams", name: "teams" },
  { url: "http://localhost:8080/irm/members", name: "members" },
  { url: "http://localhost:8080/readiness", name: "readiness" },
  { url: "http://localhost:8080/vod", name: "vod" },
  { url: "http://localhost:8080/admin", name: "admin" },
];

interface PageResult {
  name: string;
  url: string;
  errors: string[];
  warnings: string[];
  screenshot: string;
}

const results: PageResult[] = [];

for (const page of PAGES) {
  const result: PageResult = {
    name: page.name,
    url: page.url,
    errors: [],
    warnings: [],
    screenshot: "",
  };

  try {
    console.log(`\n--- Navigating to ${page.name} (${page.url}) ---`);

    await cd.navigate_page({ url: page.url });
    await wait(2000);

    // Collect console messages (errors and warnings)
    try {
      const msgResult = await cd.list_console_messages({
        types: ["error", "warning"],
      });
      const raw = text(msgResult);

      // Parse messages from the result text
      if (!raw.includes("No console messages")) {
        // Split by message boundaries — look for lines with level indicators
        const lines = raw.split("\n");
        for (const line of lines) {
          const trimmed = line.trim();
          if (!trimmed) continue;
          // Match patterns like "error: ...", "warning: ...", or lines containing [error]/[warning]
          if (
            /\berror\b/i.test(trimmed) &&
            !/no console messages/i.test(trimmed)
          ) {
            result.errors.push(trimmed);
          } else if (
            /\bwarn(ing)?\b/i.test(trimmed) &&
            !/no console messages/i.test(trimmed)
          ) {
            result.warnings.push(trimmed);
          }
        }
      }
    } catch (e: any) {
      console.log(`  Console message collection failed: ${e.message || e}`);
    }

    // Take screenshot
    try {
      const screenshotFile = `E05-console-${page.name}.png`;
      await cd.take_screenshot({
        filePath: `${SCREENSHOT_DIR}/${screenshotFile}`,
      });
      result.screenshot = screenshotFile;
      console.log(`  Screenshot: ${screenshotFile}`);
    } catch (e: any) {
      console.log(`  Screenshot failed: ${e.message || e}`);
    }

    console.log(
      `  Errors: ${result.errors.length}, Warnings: ${result.warnings.length}`,
    );
  } catch (e: any) {
    console.log(`  FAILED to process ${page.name}: ${e.message || e}`);
  }

  results.push(result);
}

// === Summary ===
console.log("\n\n========================================");
console.log("  Phase 19.7.A.4 — Console Errors Report");
console.log("========================================\n");

let totalErrors = 0;
let totalWarnings = 0;
let totalScreenshots = 0;

for (const r of results) {
  const status =
    r.errors.length > 0 ? "FAIL" : r.warnings.length > 0 ? "WARN" : "PASS";
  console.log(
    `[${status}] ${r.name.padEnd(12)} — Errors: ${r.errors.length}, Warnings: ${r.warnings.length}`,
  );
  totalErrors += r.errors.length;
  totalWarnings += r.warnings.length;
  if (r.screenshot) totalScreenshots++;
}

console.log(`\n--- Totals ---`);
console.log(`Pages checked:    ${results.length}`);
console.log(`Screenshots:      ${totalScreenshots}`);
console.log(`Total errors:     ${totalErrors}`);
console.log(`Total warnings:   ${totalWarnings}`);

if (totalErrors > 0) {
  console.log(`\n--- Error Details ---`);
  for (const r of results) {
    if (r.errors.length > 0) {
      console.log(`\n  ${r.name} (${r.url}):`);
      for (const msg of r.errors) {
        console.log(`    - ${msg.slice(0, 200)}`);
      }
    }
  }
}

if (totalWarnings > 0) {
  console.log(`\n--- Warning Details ---`);
  for (const r of results) {
    if (r.warnings.length > 0) {
      console.log(`\n  ${r.name} (${r.url}):`);
      for (const msg of r.warnings) {
        console.log(`    - ${msg.slice(0, 200)}`);
      }
    }
  }
}

const overall = totalErrors > 0 ? "FAIL" : totalWarnings > 0 ? "WARN" : "PASS";
console.log(`\n========================================`);
console.log(`  OVERALL: ${overall}`);
console.log(`========================================`);
