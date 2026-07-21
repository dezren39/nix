// deno-lint-ignore-file no-explicit-any
/**
 * test-theme-persistence.ts — Phase 19.7.E.2: Theme persistence test
 * @example lootbox test-theme-persistence.ts
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

const SCREENSHOT_DIR =
  "/Users/drewry.pope/.config/nix/.opencode/worktrees/theme-align/features/2026-04-06_0019.0_theme-alignment-bootstrap-removal/screenshots";

async function screenshot(name: string): Promise<void> {
  try {
    await cd.take_screenshot({ filePath: `${SCREENSHOT_DIR}/${name}` });
    console.log(`  Screenshot: ${name}`);
    screenshotCount++;
  } catch (e: any) {
    console.log(`  Screenshot FAILED (${name}): ${e.message || e}`);
  }
}

let screenshotCount = 0;
let assertionCount = 0;
let passCount = 0;
let failCount = 0;

function assert(label: string, actual: any, expected: any): boolean {
  assertionCount++;
  const pass = actual === expected;
  if (pass) {
    passCount++;
    console.log(`  [PASS] ${label}: ${JSON.stringify(actual)}`);
  } else {
    failCount++;
    console.log(
      `  [FAIL] ${label}: got ${JSON.stringify(actual)}, expected ${JSON.stringify(expected)}`,
    );
  }
  return pass;
}

try {
  // Step 1-3: Navigate to home, wait, screenshot
  console.log("\n=== Step 1: Navigate to home ===");
  await cd.navigate_page({ url: "http://localhost:8080/" });
  await new Promise((r) => setTimeout(r, 2000));
  await screenshot("E02-persistence-01-initial.png");

  // Step 4-6: Set theme to dracula, wait, screenshot
  console.log("\n=== Step 2: Set theme to dracula ===");
  await evalJs("() => window.ThemeUtil.setTheme('dracula')");
  await new Promise((r) => setTimeout(r, 300));
  await screenshot("E02-persistence-02-dracula-set.png");

  // Step 7: Verify localStorage
  console.log("\n=== Step 3: Verify localStorage ===");
  const lsTheme = await evalJs("() => localStorage.getItem('op-theme')");
  assert("localStorage op-theme", lsTheme, "dracula");

  // Step 8-10: Navigate to teams, wait, screenshot
  console.log("\n=== Step 4: Navigate to /teams (cross-page persistence) ===");
  await cd.navigate_page({ url: "http://localhost:8080/teams" });
  await new Promise((r) => setTimeout(r, 1000));
  await screenshot("E02-persistence-03-teams-dracula.png");

  // Step 11: Verify theme still active on different page
  console.log("\n=== Step 5: Verify theme on /teams ===");
  const teamsDataTheme = await evalJs(
    "() => document.documentElement.getAttribute('data-theme')",
  );
  assert("data-theme on /teams", teamsDataTheme, "dracula");

  // Step 12-14: Navigate back to home (simulate reload), wait, screenshot
  console.log("\n=== Step 6: Navigate back to home (simulate reload) ===");
  await cd.navigate_page({ url: "http://localhost:8080/" });
  await new Promise((r) => setTimeout(r, 1000));
  await screenshot("E02-persistence-04-reload-dracula.png");

  // Step 15: Verify theme persisted after navigation
  console.log("\n=== Step 7: Verify theme after reload ===");
  const reloadDataTheme = await evalJs(
    "() => document.documentElement.getAttribute('data-theme')",
  );
  assert("data-theme after reload", reloadDataTheme, "dracula");

  // Step 16-18: Set dark theme to synthwave, verify localStorage
  console.log("\n=== Step 8: Set dark theme to synthwave ===");
  await evalJs("() => window.ThemeUtil.setTheme('synthwave')");
  await new Promise((r) => setTimeout(r, 300));
  const lsDarkTheme = await evalJs(
    "() => localStorage.getItem('op-theme-dark')",
  );
  assert("localStorage op-theme-dark", lsDarkTheme, "synthwave");

  // Step 19-21: Toggle to light mode, wait, screenshot
  console.log("\n=== Step 9: Toggle to light mode ===");
  await evalJs("() => window.ThemeUtil.toggleMode()");
  await new Promise((r) => setTimeout(r, 300));
  await screenshot("E02-persistence-05-light-mode.png");

  // Step 22: Verify mode is light
  console.log("\n=== Step 10: Verify light mode ===");
  const lightMode = await evalJs("() => window.ThemeUtil.getMode()");
  assert("mode after toggle", lightMode, "light");

  // Step 23-25: Toggle back to dark, wait, screenshot
  console.log("\n=== Step 11: Toggle back to dark mode ===");
  await evalJs("() => window.ThemeUtil.toggleMode()");
  await new Promise((r) => setTimeout(r, 300));
  await screenshot("E02-persistence-06-back-to-dark.png");

  // Step 26: Verify theme is synthwave (our last dark theme)
  console.log("\n=== Step 12: Verify synthwave restored ===");
  const darkDataTheme = await evalJs(
    "() => document.documentElement.getAttribute('data-theme')",
  );
  assert("data-theme back to dark", darkDataTheme, "synthwave");
} catch (e: any) {
  console.log(`\nFATAL ERROR: ${e.message || e}`);
  failCount++;
}

// === Summary ===
console.log("\n\n========================================");
console.log("  Phase 19.7.E.2 — Theme Persistence Report");
console.log("========================================\n");
console.log(`Screenshots taken: ${screenshotCount}`);
console.log(`Assertions:        ${assertionCount}`);
console.log(`  Passed:          ${passCount}`);
console.log(`  Failed:          ${failCount}`);

const overall = failCount === 0 ? "PASS" : "FAIL";
console.log(`\n========================================`);
console.log(`  OVERALL: ${overall}`);
console.log(`========================================`);
