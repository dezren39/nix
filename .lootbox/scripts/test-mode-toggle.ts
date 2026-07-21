// Phase 19.7.E.3 — Test light/dark mode toggle thoroughly
const cd = tools.mcp_chrome_devtools;
const SHOT_DIR =
  "/Users/drewry.pope/.config/nix/.opencode/worktrees/theme-align/features/2026-04-06_0019.0_theme-alignment-bootstrap-removal/screenshots";

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

async function shot(name: string): Promise<void> {
  await cd.take_screenshot({ filePath: `${SHOT_DIR}/${name}` });
  shotCount++;
  console.log(`  [SCREENSHOT] ${name}`);
}

async function wait(ms: number): Promise<void> {
  await evalJs(
    `async () => { await new Promise(r => setTimeout(r, ${ms})); return "waited ${ms}ms"; }`,
  );
}

async function getState(): Promise<{
  mode: string;
  theme: string;
  dataTheme: string;
}> {
  return await evalJs(`async () => {
    const dt = document.documentElement.getAttribute('data-theme') || '';
    const mode = localStorage.getItem('op-theme-mode') || '';
    const theme = dt;
    return { mode, theme, dataTheme: dt };
  }`);
}

let shotCount = 0;
let pass = 0;
let fail = 0;
const results: string[] = [];

function check(label: string, actual: any, expected: any): void {
  const ok = actual === expected;
  const status = ok ? "PASS" : "FAIL";
  if (ok) pass++;
  else fail++;
  const msg = `  [${status}] ${label}: got "${actual}", expected "${expected}"`;
  console.log(msg);
  results.push(msg);
}

function checkContains(label: string, actual: string, substr: string): void {
  const ok = actual?.includes(substr);
  const status = ok ? "PASS" : "FAIL";
  if (ok) pass++;
  else fail++;
  const msg = `  [${status}] ${label}: "${actual}" ${ok ? "contains" : "does NOT contain"} "${substr}"`;
  console.log(msg);
  results.push(msg);
}

// --- Begin test ---
console.log("=== Phase 19.7.E.3: Light/Dark Mode Toggle Test ===\n");

// Step 1-2: Navigate and wait
console.log("Step 1: Navigate to http://localhost:8080/");
await cd.navigate_page({ url: "http://localhost:8080/" });
await wait(1500);

// Step 3-4: Initial state
console.log("\nStep 3-4: Get initial state");
const initial = await getState();
console.log(
  `  Initial state: mode="${initial.mode}", theme="${initial.theme}", data-theme="${initial.dataTheme}"`,
);
await shot("E03-mode-01-initial.png");

// Step 5-8: Set dark theme to 'forest'
console.log("\nStep 5-8: Set dark theme to 'forest'");
await evalJs(`async () => { ThemeUtil.setTheme('forest'); return 'done'; }`);
await wait(300);
await shot("E03-mode-02-forest-dark.png");
const afterForest = await getState();
check("mode after setTheme('forest')", afterForest.mode, "dark");
check("theme after setTheme('forest')", afterForest.theme, "forest");
check("data-theme after setTheme('forest')", afterForest.dataTheme, "forest");

// Step 9-12: Toggle to light
console.log("\nStep 9-12: Toggle to light mode");
await evalJs(`async () => { ThemeUtil.toggleMode(); return 'done'; }`);
await wait(300);
await shot("E03-mode-03-toggled-light.png");
const afterToggleLight = await getState();
check("mode after toggleMode() to light", afterToggleLight.mode, "light");
console.log(`  Light theme is: "${afterToggleLight.theme}"`);

// Step 13-16: Set light theme to 'cupcake'
console.log("\nStep 13-16: Set light theme to 'cupcake'");
await evalJs(`async () => { ThemeUtil.setTheme('cupcake'); return 'done'; }`);
await wait(300);
await shot("E03-mode-04-cupcake-light.png");
const lsLightTheme = await evalJs(
  `async () => localStorage.getItem('op-theme-light')`,
);
check("localStorage op-theme-light", lsLightTheme, "cupcake");

// Step 17-20: Toggle back to dark
console.log("\nStep 17-20: Toggle back to dark — should restore 'forest'");
await evalJs(`async () => { ThemeUtil.toggleMode(); return 'done'; }`);
await wait(300);
await shot("E03-mode-05-back-to-forest.png");
const backToDark = await getState();
check("theme after toggle back to dark", backToDark.theme, "forest");
check("mode after toggle back to dark", backToDark.mode, "dark");

// Step 21-24: Toggle to light again — should restore 'cupcake'
console.log("\nStep 21-24: Toggle to light again — should restore 'cupcake'");
await evalJs(`async () => { ThemeUtil.toggleMode(); return 'done'; }`);
await wait(300);
await shot("E03-mode-06-back-to-cupcake.png");
const backToLight = await getState();
check("theme after toggle back to light", backToLight.theme, "cupcake");
check("mode after toggle back to light", backToLight.mode, "light");

// Step 25-27: Set light theme to 'lemonade'
console.log("\nStep 25-27: Set light theme to 'lemonade'");
await evalJs(`async () => { ThemeUtil.setTheme('lemonade'); return 'done'; }`);
await wait(300);
await shot("E03-mode-07-lemonade.png");
const afterLemonade = await getState();
check("theme after setTheme('lemonade')", afterLemonade.theme, "lemonade");

// Step 28-31: Toggle to dark — should still be 'forest', not 'lemonade'
console.log(
  "\nStep 28-31: Toggle to dark — dark theme should still be 'forest'",
);
await evalJs(`async () => { ThemeUtil.toggleMode(); return 'done'; }`);
await wait(300);
await shot("E03-mode-08-dark-still-forest.png");
const finalDark = await getState();
check(
  "theme after toggle to dark (should be forest, not lemonade)",
  finalDark.theme,
  "forest",
);
check("mode after final toggle to dark", finalDark.mode, "dark");

// Summary
console.log("\n=== SUMMARY ===");
console.log(`Screenshots taken: ${shotCount}`);
console.log(`Pass: ${pass}  Fail: ${fail}  Total: ${pass + fail}`);
console.log(
  fail === 0 ? "\n*** ALL TESTS PASSED ***" : "\n*** SOME TESTS FAILED ***",
);
results.forEach((r) => console.log(r));
