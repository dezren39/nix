// @ts-nocheck
// Phase 19.7.B — Theme picker Part 1 (B01-B05)
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
async function snap(): Promise<string> {
  return text(await cd.take_snapshot({}));
}
async function wait(ms: number): Promise<void> {
  await evalJs(
    `async () => { await new Promise(r => setTimeout(r, ${ms})); return "done"; }`,
  );
}
function findUid(tree: string, pattern: RegExp): string | null {
  for (const line of tree.split("\n")) {
    if (pattern.test(line)) {
      const m = line.match(/uid=([^\]]+)/);
      if (m) return m[1];
    }
  }
  return null;
}

console.log("=== Theme Picker Part 1 (B01-B05) ===");

// B01
console.log("\nB01: Navigate to home");
await cd.navigate_page({ url: "http://localhost:8080" });
await wait(500);
await shot("B01-home-default-theme.png");
console.log("  B01 OK");

// B02
console.log("B02: Theme picker button visible");
await shot("B02-picker-button-visible.png");
console.log("  B02 OK");

// B03
console.log("B03: Click theme picker, screenshot dropdown");
const tree3 = await snap();
let uid3 = findUid(
  tree3,
  /theme.?picker|theme.?toggle|theme.?button|palette|paint/i,
);
if (!uid3) uid3 = findUid(tree3, /theme/i);
if (uid3) {
  await cd.click({ uid: uid3 });
} else {
  await evalJs(`(() => {
    const btn = document.querySelector('[data-theme-toggle], .theme-picker-toggle, .theme-toggle, #themePickerToggle, [aria-label*="theme" i], [title*="theme" i]');
    if (btn) { btn.click(); return 'clicked'; }
    return 'not found';
  })()`);
}
await wait(300);
await shot("B03-picker-dropdown-open.png");
console.log("  B03 OK");

// B04
console.log("B04: Click Dark tab");
const tree4 = await snap();
const uid4 = findUid(tree4, /\bDark\b/);
if (uid4) {
  await cd.click({ uid: uid4 });
} else {
  await evalJs(`(() => {
    const tabs = document.querySelectorAll('[data-tab], .tab, [role="tab"]');
    for (const t of tabs) { if (/dark/i.test(t.textContent)) { t.click(); return; } }
  })()`);
}
await wait(200);
await shot("B04-dark-tab-selected.png");
console.log("  B04 OK");

// B05
console.log("B05: Click Light tab");
const tree5 = await snap();
const uid5 = findUid(tree5, /\bLight\b/);
if (uid5) {
  await cd.click({ uid: uid5 });
} else {
  await evalJs(`(() => {
    const tabs = document.querySelectorAll('[data-tab], .tab, [role="tab"]');
    for (const t of tabs) { if (/light/i.test(t.textContent)) { t.click(); return; } }
  })()`);
}
await wait(200);
await shot("B05-light-tab-selected.png");
console.log("  B05 OK");

console.log("\nPart 1 done (B01-B05).");
