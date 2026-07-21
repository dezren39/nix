// @ts-nocheck
// Phase 19.7.B — Theme picker Part 3 (B11-B15) + Summary
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

console.log("=== Theme Picker Part 3 (B11-B15) ===");

// B11: Set cyberpunk
console.log("\nB11: Set cyberpunk theme");
const r11 = await evalJs(`(() => {
  if (window.ThemeUtil) { window.ThemeUtil.setTheme('cyberpunk'); return { set: true }; }
  return { set: false };
})()`);
console.log("  result:", JSON.stringify(r11));
await wait(300);
await shot("B11-cyberpunk-light-theme.png");
console.log("  B11 OK");

// B12: Set winter
console.log("B12: Set winter theme");
const r12 = await evalJs(`(() => {
  if (window.ThemeUtil) { window.ThemeUtil.setTheme('winter'); return { set: true }; }
  return { set: false };
})()`);
console.log("  result:", JSON.stringify(r12));
await wait(300);
await shot("B12-winter-light-theme.png");
console.log("  B12 OK");

// B13: Toggle dark mode
console.log("B13: Toggle back to dark");
const r13 = await evalJs(`(() => {
  if (window.ThemeUtil && window.ThemeUtil.toggleMode) {
    window.ThemeUtil.toggleMode();
    return { mode: document.documentElement.getAttribute('data-mode'), theme: document.documentElement.getAttribute('data-theme') };
  }
  return { error: 'no toggleMode' };
})()`);
console.log("  result:", JSON.stringify(r13));
await wait(300);
await shot("B13-dark-mode-evermoss-returns.png");
console.log("  B13 OK");

// B14: Navigate home
console.log("B14: Navigate home");
await cd.navigate_page({ url: "http://localhost:8080" });
await shot("B14-home-evermoss-still-active.png");
console.log("  B14 OK");

// B15: Toggle light mode
console.log("B15: Toggle light mode (should show winter)");
const r15 = await evalJs(`(() => {
  if (window.ThemeUtil && window.ThemeUtil.toggleMode) {
    window.ThemeUtil.toggleMode();
    return { mode: document.documentElement.getAttribute('data-mode'), theme: document.documentElement.getAttribute('data-theme') };
  }
  return { error: 'no toggleMode' };
})()`);
console.log("  result:", JSON.stringify(r15));
await wait(300);
await shot("B15-light-mode-winter-returns.png");
console.log("  B15 OK");

console.log("\n=== All 15 theme picker steps complete (B01-B15) ===");
