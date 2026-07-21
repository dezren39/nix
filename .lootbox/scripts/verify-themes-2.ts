// deno-lint-ignore-file no-explicit-any
/**
 * verify-themes-2.ts — Phase 19.6.5: Theme Picker UI
 * @example lootbox verify-themes-2.ts
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

// Reset to evermoss
await evalJs("() => window.ThemeUtil && window.ThemeUtil.setTheme('evermoss')");

// Check picker container
const picker = await evalJs(`() => {
  var el = document.getElementById('theme-picker-container');
  if (!el) return null;
  return {
    exists: true,
    childCount: el.children.length,
    hasDropdown: el.querySelector('.dropdown') !== null,
    hasTriggerBtn: el.querySelector('button[aria-label="Change theme"]') !== null,
  };
}`);
console.log("Picker container:", JSON.stringify(picker));

// Open picker
await evalJs(
  "() => { var b = document.querySelector('button[aria-label=\"Change theme\"]'); if (b) b.click(); }",
);
await new Promise((r) => setTimeout(r, 300));

// Check dropdown state
const ddState = await evalJs(`() => {
  var dd = document.querySelector('#theme-picker-container .dropdown');
  if (!dd) return null;
  var panel = dd.querySelector('.dropdown-content');
  return {
    open: dd.classList.contains('dropdown-open'),
    visible: panel ? (panel.style.display !== 'none') : false,
  };
}`);
console.log("Dropdown state:", JSON.stringify(ddState));

await cd.take_screenshot({});
console.log("Screenshot (picker open): captured");

// Check listed themes, tabs, search
const [listedThemes, tabs, hasSearch] = await Promise.all([
  evalJs(
    "() => { var b = document.querySelectorAll('#theme-picker-container [data-theme-name]'); return Array.from(b).map(function(x){return x.getAttribute('data-theme-name')}).slice(0,20); }",
  ),
  evalJs(
    "() => { var t = document.querySelectorAll('#theme-picker-container .tab'); return Array.from(t).map(function(x){return x.textContent}); }",
  ),
  evalJs(
    "() => document.querySelector('#theme-picker-container input[type=\"text\"]') !== null",
  ),
]);
console.log("Listed themes (first 20):", JSON.stringify(listedThemes));
console.log("Tabs:", JSON.stringify(tabs));
console.log("Search input:", hasSearch);

// Select blueprint via picker
await evalJs(
  "() => { var b = document.querySelector('#theme-picker-container [data-theme-name=\"blueprint\"]'); if (b) b.click(); }",
);
await new Promise((r) => setTimeout(r, 300));

const afterSelect = await evalJs(
  "() => document.documentElement.getAttribute('data-theme')",
);
console.log("After selecting blueprint:", afterSelect);

await cd.take_screenshot({});
console.log("Screenshot (blueprint selected): captured");

console.log("\n=== Phase 19.6.5 COMPLETE ===");
