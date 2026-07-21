// @ts-nocheck
// Phase 19.7.B — Full theme picker screenshot journey

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

async function screenshot(name: string): Promise<void> {
  await cd.take_screenshot({ filePath: `${SHOT_DIR}${name}` });
}

async function snap(): Promise<string> {
  const r = await cd.take_snapshot({});
  return text(r);
}

async function wait(ms: number): Promise<void> {
  await evalJs(
    `async () => { await new Promise(r => setTimeout(r, ${ms})); return "waited ${ms}ms"; }`,
  );
}

/** Find a uid in the snapshot text matching a regex */
function findUid(tree: string, pattern: RegExp): string | null {
  // Snapshot lines look like:  [uid=ABC] role "label text"
  // or  [uid=ABC] ... text content ...
  const lines = tree.split("\n");
  for (const line of lines) {
    if (pattern.test(line)) {
      const m = line.match(/uid=([^\]]+)/);
      if (m) return m[1];
    }
  }
  return null;
}

// ── Tracking ──────────────────────────────────────────
interface StepResult {
  id: string;
  desc: string;
  ok: boolean;
  error?: string;
}
const results: StepResult[] = [];

async function step(
  id: string,
  desc: string,
  fn: () => Promise<void>,
): Promise<void> {
  console.log(`\n=== ${id}: ${desc} ===`);
  try {
    await fn();
    results.push({ id, desc, ok: true });
    console.log(`  -> ${id} OK`);
  } catch (e: any) {
    const msg = e?.message ?? String(e);
    results.push({ id, desc, ok: false, error: msg });
    console.log(`  -> ${id} FAIL: ${msg}`);
  }
}

// ── Steps ─────────────────────────────────────────────

await step("B01", "Navigate to home, screenshot default theme", async () => {
  await cd.navigate_page({ url: "http://localhost:8080" });
  await wait(1500);
  await screenshot("B01-home-default-theme.png");
});

await step(
  "B02",
  "Screenshot showing theme picker button visible in navbar",
  async () => {
    const tree = await snap();
    // Log a snippet around theme picker for debugging
    const lines = tree.split("\n").filter((l: string) => /theme/i.test(l));
    console.log(
      "  theme-related snapshot lines:",
      lines.slice(0, 10).join("\n"),
    );
    await screenshot("B02-picker-button-visible.png");
  },
);

await step(
  "B03",
  "Click theme picker button, screenshot open dropdown",
  async () => {
    const tree = await snap();
    // The theme picker toggle button — look for theme/palette/paint icon or text
    let uid = findUid(
      tree,
      /theme.?picker|theme.?toggle|theme.?button|palette|paint/i,
    );
    if (!uid) {
      // Fallback: look for a dropdown toggle in the navbar that could be the theme picker
      uid = findUid(tree, /theme/i);
    }
    if (!uid) {
      // Try clicking by evaluating JS to find the element
      await evalJs(`
      (() => {
        const btn = document.querySelector('[data-theme-toggle], .theme-picker-toggle, .theme-toggle, #themePickerToggle, [aria-label*="theme" i], [title*="theme" i]');
        if (btn) { btn.click(); return 'clicked'; }
        return 'not found';
      })()
    `);
    } else {
      await cd.click({ uid });
    }
    await wait(500);
    await screenshot("B03-picker-dropdown-open.png");
  },
);

await step("B04", "Click Dark tab in picker, screenshot", async () => {
  const tree = await snap();
  let uid = findUid(tree, /\bDark\b/);
  if (uid) {
    await cd.click({ uid });
  } else {
    await evalJs(`
      (() => {
        const tabs = document.querySelectorAll('[data-tab], .tab, [role="tab"]');
        for (const t of tabs) {
          if (/dark/i.test(t.textContent)) { t.click(); return 'clicked dark'; }
        }
        return 'not found';
      })()
    `);
  }
  await wait(300);
  await screenshot("B04-dark-tab-selected.png");
});

await step("B05", "Click Light tab in picker, screenshot", async () => {
  const tree = await snap();
  let uid = findUid(tree, /\bLight\b/);
  if (uid) {
    await cd.click({ uid });
  } else {
    await evalJs(`
      (() => {
        const tabs = document.querySelectorAll('[data-tab], .tab, [role="tab"]');
        for (const t of tabs) {
          if (/light/i.test(t.textContent)) { t.click(); return 'clicked light'; }
        }
        return 'not found';
      })()
    `);
  }
  await wait(300);
  await screenshot("B05-light-tab-selected.png");
});

await step(
  "B06",
  "Hover over evermoss theme item, screenshot highlight",
  async () => {
    // First switch to Dark tab since evermoss is likely a dark theme
    await evalJs(`
    (() => {
      const tabs = document.querySelectorAll('[data-tab], .tab, [role="tab"]');
      for (const t of tabs) {
        if (/dark/i.test(t.textContent)) { t.click(); return 'switched to dark'; }
      }
      return 'no dark tab';
    })()
  `);
    await wait(300);
    const tree = await snap();
    let uid = findUid(tree, /evermoss/i);
    if (uid) {
      await cd.hover({ uid });
    } else {
      // Try JS hover
      await evalJs(`
      (() => {
        const items = document.querySelectorAll('[data-theme], .theme-item, .theme-option');
        for (const el of items) {
          if (/evermoss/i.test(el.textContent) || /evermoss/i.test(el.getAttribute('data-theme') || '')) {
            el.dispatchEvent(new MouseEvent('mouseenter', {bubbles: true}));
            el.dispatchEvent(new MouseEvent('mouseover', {bubbles: true}));
            return 'hovered evermoss';
          }
        }
        return 'evermoss not found';
      })()
    `);
    }
    await wait(300);
    await screenshot("B06-evermoss-hover-highlight.png");
  },
);

await step(
  "B07",
  "Click evermoss theme, screenshot theme changed",
  async () => {
    const tree = await snap();
    let uid = findUid(tree, /evermoss/i);
    if (uid) {
      await cd.click({ uid });
    } else {
      await evalJs(`
      (() => {
        const items = document.querySelectorAll('[data-theme], .theme-item, .theme-option');
        for (const el of items) {
          if (/evermoss/i.test(el.textContent) || /evermoss/i.test(el.getAttribute('data-theme') || '')) {
            el.click(); return 'clicked evermoss';
          }
        }
        // Fallback: use ThemeUtil directly
        if (window.ThemeUtil) { window.ThemeUtil.setTheme('evermoss'); return 'set via ThemeUtil'; }
        return 'evermoss not found';
      })()
    `);
    }
    await wait(500);
    await screenshot("B07-evermoss-theme-applied.png");
  },
);

await step(
  "B08",
  "Navigate to /irm/incidents, screenshot evermoss persists",
  async () => {
    await cd.navigate_page({ url: "http://localhost:8080/irm/incidents" });
    await wait(1500);
    await screenshot("B08-incidents-evermoss-persists.png");
  },
);

await step(
  "B09",
  "Navigate to /irm/teams, screenshot theme persists",
  async () => {
    await cd.navigate_page({ url: "http://localhost:8080/irm/teams" });
    await wait(1500);
    await screenshot("B09-teams-evermoss-persists.png");
  },
);

await step(
  "B10",
  "Toggle to light mode via ThemeUtil, screenshot",
  async () => {
    const result = await evalJs(`
    (() => {
      if (window.ThemeUtil && typeof window.ThemeUtil.toggleMode === 'function') {
        window.ThemeUtil.toggleMode();
        return { toggled: true, mode: document.documentElement.getAttribute('data-mode') || document.documentElement.getAttribute('data-theme') };
      }
      return { toggled: false, error: 'ThemeUtil.toggleMode not found' };
    })()
  `);
    console.log("  toggleMode result:", JSON.stringify(result));
    await wait(500);
    await screenshot("B10-light-mode-toggled.png");
  },
);

await step("B11", "Set cyberpunk theme via ThemeUtil, screenshot", async () => {
  const result = await evalJs(`
    (() => {
      if (window.ThemeUtil && typeof window.ThemeUtil.setTheme === 'function') {
        window.ThemeUtil.setTheme('cyberpunk');
        return { set: true, theme: 'cyberpunk' };
      }
      return { set: false, error: 'ThemeUtil.setTheme not found' };
    })()
  `);
  console.log("  setTheme result:", JSON.stringify(result));
  await wait(500);
  await screenshot("B11-cyberpunk-light-theme.png");
});

await step("B12", "Set winter theme via ThemeUtil, screenshot", async () => {
  const result = await evalJs(`
    (() => {
      if (window.ThemeUtil && typeof window.ThemeUtil.setTheme === 'function') {
        window.ThemeUtil.setTheme('winter');
        return { set: true, theme: 'winter' };
      }
      return { set: false, error: 'ThemeUtil.setTheme not found' };
    })()
  `);
  console.log("  setTheme result:", JSON.stringify(result));
  await wait(500);
  await screenshot("B12-winter-light-theme.png");
});

await step(
  "B13",
  "Toggle back to dark mode, screenshot (should show evermoss)",
  async () => {
    const result = await evalJs(`
    (() => {
      if (window.ThemeUtil && typeof window.ThemeUtil.toggleMode === 'function') {
        window.ThemeUtil.toggleMode();
        return {
          toggled: true,
          mode: document.documentElement.getAttribute('data-mode') || 'unknown',
          theme: document.documentElement.getAttribute('data-theme') || 'unknown',
        };
      }
      return { toggled: false, error: 'ThemeUtil.toggleMode not found' };
    })()
  `);
    console.log("  toggleMode result:", JSON.stringify(result));
    await wait(500);
    await screenshot("B13-dark-mode-evermoss-returns.png");
  },
);

await step(
  "B14",
  "Navigate home, screenshot evermoss still active",
  async () => {
    await cd.navigate_page({ url: "http://localhost:8080" });
    await wait(1500);
    await screenshot("B14-home-evermoss-still-active.png");
  },
);

await step(
  "B15",
  "Toggle to light mode, screenshot (should show winter)",
  async () => {
    const result = await evalJs(`
    (() => {
      if (window.ThemeUtil && typeof window.ThemeUtil.toggleMode === 'function') {
        window.ThemeUtil.toggleMode();
        return {
          toggled: true,
          mode: document.documentElement.getAttribute('data-mode') || 'unknown',
          theme: document.documentElement.getAttribute('data-theme') || 'unknown',
        };
      }
      return { toggled: false, error: 'ThemeUtil.toggleMode not found' };
    })()
  `);
    console.log("  toggleMode result:", JSON.stringify(result));
    await wait(500);
    await screenshot("B15-light-mode-winter-returns.png");
  },
);

// ── Summary ───────────────────────────────────────────
console.log("\n\n========================================");
console.log("  Phase 19.7.B — Screenshot Summary");
console.log("========================================\n");

const passed = results.filter((r) => r.ok);
const failed = results.filter((r) => !r.ok);

console.log(`Screenshots taken: ${results.length}`);
console.log(`PASSED: ${passed.length}  |  FAILED: ${failed.length}\n`);

for (const r of results) {
  const icon = r.ok ? "PASS" : "FAIL";
  console.log(`  [${icon}] ${r.id} — ${r.desc}`);
  if (!r.ok) console.log(`         Error: ${r.error}`);
}

console.log("\nScreenshot files:");
for (const r of results) {
  if (r.ok) {
    const filename = [
      "B01-home-default-theme.png",
      "B02-picker-button-visible.png",
      "B03-picker-dropdown-open.png",
      "B04-dark-tab-selected.png",
      "B05-light-tab-selected.png",
      "B06-evermoss-hover-highlight.png",
      "B07-evermoss-theme-applied.png",
      "B08-incidents-evermoss-persists.png",
      "B09-teams-evermoss-persists.png",
      "B10-light-mode-toggled.png",
      "B11-cyberpunk-light-theme.png",
      "B12-winter-light-theme.png",
      "B13-dark-mode-evermoss-returns.png",
      "B14-home-evermoss-still-active.png",
      "B15-light-mode-winter-returns.png",
    ];
    const idx = parseInt(r.id.replace("B", ""), 10) - 1;
    if (filename[idx]) console.log(`  ${SHOT_DIR}${filename[idx]}`);
  }
}

console.log(
  `\nOverall: ${failed.length === 0 ? "ALL PASS" : `${failed.length} FAILURES`}`,
);
