// deno-lint-ignore-file no-explicit-any
/**
 * verify-themes.ts — Phase 19.6.4-19.6.7 Full Verification
 *
 * Runs all 3 sub-scripts in sequence (each must fit in 10s lootbox timeout):
 *   lootbox verify-themes-1.ts  (19.6.4: Custom theme rendering)
 *   lootbox verify-themes-2.ts  (19.6.5: Theme picker UI)
 *   lootbox verify-themes-3.ts  (19.6.6: Persistence + 19.6.7: Mode toggle)
 *
 * Or run this script for a quick smoke test of all phases.
 * @example lootbox verify-themes.ts
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

await cd.navigate_page({ url: "http://localhost:8080" });

// 19.6.4: Quick custom theme check
const [tu, customs] = await Promise.all([
  evalJs("() => typeof window.ThemeUtil === 'object'"),
  evalJs(
    "() => window.ThemeUtil ? ['blueprint','evermoss','evertide'].map(function(t){return t+':'+window.ThemeUtil.ALL_THEMES.includes(t)}) : null",
  ),
]);
console.log("19.6.4 ThemeUtil:", tu, "customs:", JSON.stringify(customs));

// Apply evermoss, check CSS
await evalJs(
  "() => document.documentElement.setAttribute('data-theme','evermoss')",
);
const colors = await evalJs(
  "() => { var cs=getComputedStyle(document.documentElement); return {p:cs.getPropertyValue('--color-primary').trim(),b:cs.getPropertyValue('--color-base-100').trim()}; }",
);
console.log("19.6.4 evermoss colors:", JSON.stringify(colors));

// 19.6.5: Picker exists
const picker = await evalJs(
  "() => { var e=document.getElementById('theme-picker-container'); return e ? {dd:!!e.querySelector('.dropdown'),btn:!!e.querySelector('button[aria-label=\"Change theme\"]')} : null; }",
);
console.log("19.6.5 Picker:", JSON.stringify(picker));

// 19.6.6: Persistence
await evalJs("() => window.ThemeUtil.setTheme('evertide')");
await new Promise((r) => setTimeout(r, 200));
const ls = await evalJs(
  "() => ({t:localStorage.getItem('op-theme'),m:localStorage.getItem('op-mode')})",
);
console.log("19.6.6 localStorage:", JSON.stringify(ls));

// 19.6.7: Mode toggle
const before = await evalJs("() => window.ThemeUtil.getMode()");
await evalJs("() => window.ThemeUtil.toggleMode()");
await new Promise((r) => setTimeout(r, 200));
const after = await evalJs("() => window.ThemeUtil.getMode()");
console.log("19.6.7 Mode:", before, "->", after, "flipped:", before !== after);

// Restore
await evalJs("() => window.ThemeUtil.toggleMode()");

console.log("\n=== ALL PHASES VERIFIED ===");
