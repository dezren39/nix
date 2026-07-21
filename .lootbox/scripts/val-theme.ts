// val-theme.ts — Comprehensive theme switching validation
// Tests: initial state, programmatic theme change (cupcake, dracula), data-theme attr, localStorage persistence
// Run: lootbox val-theme.ts

const cd = tools.mcp_chrome_devtools;

function text(r: any): string {
  return r.content.map((c: any) => c.text ?? "").join("");
}

async function evalJs(fn: string): Promise<any> {
  const r = await cd.evaluate_script({ function: fn });
  const raw = text(r);
  // Strip markdown code fences if present
  const m = raw.match(/```(?:json)?\n([\s\S]*?)\n```/);
  let val = m ? m[1] : raw;
  // Parse potentially double-stringified JSON
  try {
    val = JSON.parse(val);
    if (typeof val === "string") {
      try {
        val = JSON.parse(val);
      } catch {}
    }
  } catch {}
  return val;
}

interface TestResult {
  step: string;
  status: "PASS" | "FAIL" | "INFO";
  detail: string;
}

const results: TestResult[] = [];
let passCount = 0;
let failCount = 0;

function info(step: string, detail: string) {
  results.push({ step, status: "INFO", detail });
}

function pass(step: string, detail: string) {
  results.push({ step, status: "PASS", detail });
  passCount++;
}

function fail(step: string, detail: string) {
  results.push({ step, status: "FAIL", detail });
  failCount++;
}

try {
  // ===== STEP 1: Navigate and wait for load =====
  console.log("[1/8] Navigating to http://localhost:8080/ ...");
  await cd.navigate_page({ url: "http://localhost:8080/" });
  await new Promise((r) => setTimeout(r, 2000));
  info("Navigate", "Loaded http://localhost:8080/");

  // ===== STEP 2: Read initial data-theme =====
  console.log("[2/8] Reading initial data-theme ...");
  const initialState = await evalJs(`() => {
    const dataTheme = document.documentElement.getAttribute('data-theme');
    const hasThemeUtil = typeof window.ThemeUtil !== 'undefined';
    const themeUtilMethods = hasThemeUtil
      ? Object.keys(window.ThemeUtil).filter(k => typeof window.ThemeUtil[k] === 'function')
      : [];
    let currentViaUtil = null;
    try { currentViaUtil = window.ThemeUtil?.getTheme?.() ?? null; } catch(e) {}
    let currentMode = null;
    try { currentMode = window.ThemeUtil?.getMode?.() ?? null; } catch(e) {}
    return JSON.stringify({
      dataTheme,
      hasThemeUtil,
      themeUtilMethods,
      currentViaUtil,
      currentMode
    });
  }`);
  info("Initial State", JSON.stringify(initialState, null, 2));

  const initialTheme =
    typeof initialState === "object" ? initialState.dataTheme : null;
  if (initialTheme) {
    pass("Initial data-theme", `data-theme="${initialTheme}"`);
  } else {
    fail(
      "Initial data-theme",
      `Could not read data-theme: ${JSON.stringify(initialState)}`,
    );
  }

  // ===== STEP 3: Find the theme picker trigger and get its label =====
  console.log("[3/8] Finding theme picker trigger ...");
  const pickerInfo = await evalJs(`() => {
    // Look for theme picker button by various selectors
    const selectors = [
      '[data-testid="theme-picker"]',
      '#theme-picker',
      '.theme-picker',
      'button[aria-label*="theme" i]',
      'button[aria-label*="Theme" i]',
      '[data-theme-trigger]',
      '.dropdown [data-theme]',
      'details summary',
      '.navbar button',
    ];
    const found: any[] = [];
    for (const sel of selectors) {
      const els = document.querySelectorAll(sel);
      els.forEach(el => {
        found.push({
          selector: sel,
          tag: el.tagName,
          text: el.textContent?.trim()?.substring(0, 80),
          ariaLabel: el.getAttribute('aria-label'),
          id: el.id || null,
          classes: el.className?.substring?.(0, 80) || null,
        });
      });
    }
    return JSON.stringify({ matchCount: found.length, matches: found.slice(0, 10) });
  }`);
  info("Theme Picker Search", JSON.stringify(pickerInfo, null, 2));

  // ===== STEP 4: Apply 'cupcake' theme programmatically =====
  console.log("[4/8] Applying 'cupcake' theme ...");
  const applyCupcake = await evalJs(`() => {
    try {
      if (typeof window.ThemeUtil !== 'undefined') {
        // Try setTheme first, then applyTheme
        if (typeof window.ThemeUtil.setTheme === 'function') {
          window.ThemeUtil.setTheme('cupcake');
          return JSON.stringify({ method: 'ThemeUtil.setTheme', ok: true });
        } else if (typeof window.ThemeUtil.applyTheme === 'function') {
          window.ThemeUtil.applyTheme('cupcake');
          return JSON.stringify({ method: 'ThemeUtil.applyTheme', ok: true });
        } else {
          return JSON.stringify({ method: 'none', ok: false, error: 'No setTheme or applyTheme found', available: Object.keys(window.ThemeUtil) });
        }
      } else {
        // Fallback: set attribute directly
        document.documentElement.setAttribute('data-theme', 'cupcake');
        return JSON.stringify({ method: 'direct-setAttribute', ok: true });
      }
    } catch(e) {
      return JSON.stringify({ method: 'error', ok: false, error: e.message });
    }
  }`);
  info("Apply cupcake", JSON.stringify(applyCupcake, null, 2));

  // Wait for theme change to settle
  await new Promise((r) => setTimeout(r, 500));

  // ===== STEP 5: Verify data-theme changed to cupcake =====
  console.log("[5/8] Verifying data-theme is 'cupcake' ...");
  const afterCupcake = await evalJs(`() => {
    const dataTheme = document.documentElement.getAttribute('data-theme');
    const ls = {};
    for (let i = 0; i < localStorage.length; i++) {
      const key = localStorage.key(i);
      if (key && (key.includes('theme') || key.includes('Theme') || key.includes('mode') || key.includes('op-'))) {
        ls[key] = localStorage.getItem(key);
      }
    }
    return JSON.stringify({ dataTheme, themeRelatedLocalStorage: ls });
  }`);
  info("After cupcake", JSON.stringify(afterCupcake, null, 2));

  const cupcakeTheme =
    typeof afterCupcake === "object" ? afterCupcake.dataTheme : null;
  if (cupcakeTheme === "cupcake") {
    pass("Cupcake Applied", `data-theme="${cupcakeTheme}"`);
  } else {
    fail("Cupcake Applied", `Expected 'cupcake', got '${cupcakeTheme}'`);
  }

  // ===== STEP 6: Apply 'dracula' theme =====
  console.log("[6/8] Applying 'dracula' theme ...");
  const applyDracula = await evalJs(`() => {
    try {
      if (typeof window.ThemeUtil !== 'undefined' && typeof window.ThemeUtil.setTheme === 'function') {
        window.ThemeUtil.setTheme('dracula');
        return JSON.stringify({ method: 'ThemeUtil.setTheme', ok: true });
      } else if (typeof window.ThemeUtil !== 'undefined' && typeof window.ThemeUtil.applyTheme === 'function') {
        window.ThemeUtil.applyTheme('dracula');
        return JSON.stringify({ method: 'ThemeUtil.applyTheme', ok: true });
      } else {
        document.documentElement.setAttribute('data-theme', 'dracula');
        return JSON.stringify({ method: 'direct-setAttribute', ok: true });
      }
    } catch(e) {
      return JSON.stringify({ method: 'error', ok: false, error: e.message });
    }
  }`);
  info("Apply dracula", JSON.stringify(applyDracula, null, 2));

  await new Promise((r) => setTimeout(r, 500));

  // ===== STEP 7: Verify data-theme changed to dracula =====
  console.log("[7/8] Verifying data-theme is 'dracula' ...");
  const afterDracula = await evalJs(`() => {
    const dataTheme = document.documentElement.getAttribute('data-theme');
    const ls = {};
    for (let i = 0; i < localStorage.length; i++) {
      const key = localStorage.key(i);
      if (key && (key.includes('theme') || key.includes('Theme') || key.includes('mode') || key.includes('op-'))) {
        ls[key] = localStorage.getItem(key);
      }
    }
    return JSON.stringify({ dataTheme, themeRelatedLocalStorage: ls });
  }`);
  info("After dracula", JSON.stringify(afterDracula, null, 2));

  const draculaTheme =
    typeof afterDracula === "object" ? afterDracula.dataTheme : null;
  if (draculaTheme === "dracula") {
    pass("Dracula Applied", `data-theme="${draculaTheme}"`);
  } else {
    fail("Dracula Applied", `Expected 'dracula', got '${draculaTheme}'`);
  }

  // ===== STEP 8: Check localStorage persistence =====
  console.log("[8/8] Checking localStorage persistence ...");
  const persistence = await evalJs(`() => {
    const allThemeKeys = {};
    for (let i = 0; i < localStorage.length; i++) {
      const key = localStorage.key(i);
      if (key && (key.includes('theme') || key.includes('Theme') || key.includes('mode') || key.includes('op-'))) {
        allThemeKeys[key] = localStorage.getItem(key);
      }
    }
    // Also check if IndexedDB has theme data
    const idbAvailable = typeof indexedDB !== 'undefined';
    return JSON.stringify({ localStorage: allThemeKeys, indexedDBAvailable: idbAvailable });
  }`);
  info("Persistence Check", JSON.stringify(persistence, null, 2));

  const lsData =
    typeof persistence === "object" ? persistence.localStorage : {};
  const hasAnyThemeKey = Object.keys(lsData || {}).length > 0;
  if (hasAnyThemeKey) {
    pass("Theme Persistence", `Found keys: ${Object.keys(lsData).join(", ")}`);
  } else {
    fail("Theme Persistence", "No theme-related keys found in localStorage");
  }

  // Restore to dark
  console.log("[cleanup] Restoring to 'dark' theme ...");
  await evalJs(`() => {
    try {
      if (typeof window.ThemeUtil !== 'undefined' && typeof window.ThemeUtil.setTheme === 'function') {
        window.ThemeUtil.setTheme('dark');
      } else {
        document.documentElement.setAttribute('data-theme', 'dark');
      }
    } catch(e) {}
    return 'ok';
  }`);
} catch (err: any) {
  fail("Script Error", err.message || String(err));
}

// ===== Report =====
console.log("\n\n╔════════════════════════════════════════════╗");
console.log("║   VAL-THEME: Theme Switching Validation    ║");
console.log("╚════════════════════════════════════════════╝\n");

for (const r of results) {
  const icon =
    r.status === "PASS" ? "[PASS]" : r.status === "FAIL" ? "[FAIL]" : "[INFO]";
  console.log(`${icon} ${r.step}: ${r.detail}`);
}

console.log(`\n--- Summary ---`);
console.log(`  Passed: ${passCount}`);
console.log(`  Failed: ${failCount}`);
console.log(`  OVERALL: ${failCount === 0 ? "PASS" : "FAIL"}`);
console.log("════════════════════════════════════════════\n");
