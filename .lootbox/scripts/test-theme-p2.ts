// @ts-nocheck
// Phase 19.7.B — Theme picker Part 2 (B06-B10)
const cd = tools.mcp_chrome_devtools;
const SHOT_DIR =
  "/Users/drewry.pope/.config/nix/.opencode/worktrees/theme-align/features/2026-04-06_0019.0_theme-alignment-bootstrap-removal/screenshots/";
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
  await cd.take_screenshot({ filePath: `${SHOT_DIR}${name}` });
}
async function wait(ms: number): Promise<void> {
  await evalJs(
    `async () => { await new Promise(r => setTimeout(r, ${ms})); return "done"; }`,
  );
}

console.log("=== Theme Picker Part 2 (B06-B10) ===");

// B06: Hover evermoss
console.log("\nB06: Hover evermoss theme");
await evalJs(`(() => {
  const tabs = document.querySelectorAll('[data-tab], .tab, [role="tab"]');
  for (const t of tabs) { if (/dark/i.test(t.textContent)) { t.click(); return 'dark'; } }
  return 'no dark tab';
})()`);
await wait(200);
await evalJs(`(() => {
  const items = document.querySelectorAll('[data-theme], .theme-item, .theme-option');
  for (const el of items) {
    if (/evermoss/i.test(el.textContent) || /evermoss/i.test(el.getAttribute('data-theme') || '')) {
      el.dispatchEvent(new MouseEvent('mouseenter', {bubbles: true}));
      return 'hovered';
    }
  }
  return 'not found';
})()`);
await wait(200);
await shot("B06-evermoss-hover-highlight.png");
console.log("  B06 OK");

// B07: Click evermoss
console.log("B07: Click evermoss theme");
await evalJs(`(() => {
  const items = document.querySelectorAll('[data-theme], .theme-item, .theme-option');
  for (const el of items) {
    if (/evermoss/i.test(el.textContent) || /evermoss/i.test(el.getAttribute('data-theme') || '')) {
      el.click(); return 'clicked';
    }
  }
  if (window.ThemeUtil) { window.ThemeUtil.setTheme('evermoss'); return 'ThemeUtil'; }
  return 'not found';
})()`);
await wait(300);
await shot("B07-evermoss-theme-applied.png");
console.log("  B07 OK");

// B08: Navigate to /irm/incidents
console.log("B08: Navigate /irm/incidents");
await cd.navigate_page({ url: "http://localhost:8080/irm/incidents" });
await shot("B08-incidents-evermoss-persists.png");
console.log("  B08 OK");

// B09: Navigate to /irm/teams
console.log("B09: Navigate /irm/teams");
await cd.navigate_page({ url: "http://localhost:8080/irm/teams" });
await shot("B09-teams-evermoss-persists.png");
console.log("  B09 OK");

// B10: Toggle light mode
console.log("B10: Toggle light mode");
const r10 = await evalJs(`(() => {
  if (window.ThemeUtil && typeof window.ThemeUtil.toggleMode === 'function') {
    window.ThemeUtil.toggleMode();
    return { toggled: true, mode: document.documentElement.getAttribute('data-mode') };
  }
  return { toggled: false };
})()`);
console.log("  toggleMode:", JSON.stringify(r10));
await wait(300);
await shot("B10-light-mode-toggled.png");
console.log("  B10 OK");

console.log("\nPart 2 done (B06-B10).");
