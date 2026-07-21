// test-theme-picker.ts — Verify theme picker portal fix works
const cd = tools.mcp_chrome_devtools;

function txt(r: any): string {
  if (!r?.content) return "";
  return r.content.map((c: any) => c.text || "").join("\n");
}

function parseJson(raw: string): any {
  // Try direct parse
  try {
    let parsed = JSON.parse(raw);
    // Handle double-stringified JSON (common with evaluate_script)
    if (typeof parsed === "string") {
      try {
        parsed = JSON.parse(parsed);
      } catch {}
    }
    return parsed;
  } catch {}
  // Try extracting JSON from markdown code block
  const m = raw.match(/```(?:json)?\n?([\s\S]+?)\n?```/);
  if (m) {
    try {
      let parsed = JSON.parse(m[1]);
      if (typeof parsed === "string")
        try {
          parsed = JSON.parse(parsed);
        } catch {}
      return parsed;
    } catch {}
  }
  // Try finding first { ... } block
  const m2 = raw.match(/\{[\s\S]*\}/);
  if (m2)
    try {
      return JSON.parse(m2[0]);
    } catch {}
  return null;
}

async function evalJs(fn: string): Promise<any> {
  const r = await cd.evaluate_script({ function: fn });
  const raw = txt(r);
  return parseJson(raw);
}

console.log("=== Theme Picker Portal Test ===\n");

// 1. Navigate
console.log("1. Navigating to /irm/dashboard...");
await cd.navigate_page({ url: "http://localhost:8080/irm/members" });
await new Promise((r) => setTimeout(r, 2000));
console.log("   Done.\n");

// 2. Check state
console.log("2. Checking theme picker state...");
const state = await evalJs(`() => {
  const container = document.getElementById('theme-picker-container');
  const portal = document.getElementById('theme-picker-portal');
  const trigger = container ? container.querySelector('button') : null;
  return JSON.stringify({
    containerExists: !!container,
    portalExists: !!portal,
    portalParent: portal ? portal.parentElement.tagName : null,
    portalDisplay: portal ? portal.style.display : null,
    triggerExists: !!trigger,
    triggerText: trigger ? trigger.textContent.trim() : null,
  });
}`);
console.log("   State:", JSON.stringify(state, null, 2));

if (!state?.portalExists) {
  console.log("FAIL: Portal not found. Theme picker didn't build.");
  console.log("END");
} else {
  console.log("   OK: Portal on", state.portalParent, "\n");

  // 3. Click trigger
  console.log("3. Clicking trigger button...");
  const click1 = await evalJs(`() => {
    const container = document.getElementById('theme-picker-container');
    const trigger = container ? container.querySelector('button') : null;
    if (!trigger) return JSON.stringify({ error: 'no trigger' });
    trigger.click();
    const portal = document.getElementById('theme-picker-portal');
    const rect = portal ? portal.getBoundingClientRect() : {};
    return JSON.stringify({
      clicked: true,
      display: portal ? portal.style.display : null,
      height: rect.height || 0,
      width: rect.width || 0,
      top: portal ? portal.style.top : null,
      left: portal ? portal.style.left : null,
      items: portal ? portal.querySelectorAll('[data-theme-name]').length : 0,
    });
  }`);
  console.log("   After click:", JSON.stringify(click1, null, 2));

  // Wait for async render
  await new Promise((r) => setTimeout(r, 500));

  // 4. Re-check after render
  console.log("\n4. Re-checking after render...");
  const state2 = await evalJs(`() => {
    const portal = document.getElementById('theme-picker-portal');
    if (!portal) return JSON.stringify({ error: 'gone' });
    const rect = portal.getBoundingClientRect();
    const cs = getComputedStyle(portal);
    return JSON.stringify({
      display: cs.display,
      visibility: cs.visibility,
      zIndex: cs.zIndex,
      position: cs.position,
      height: rect.height,
      width: rect.width,
      top: rect.top,
      left: rect.left,
      items: portal.querySelectorAll('[data-theme-name]').length,
    });
  }`);
  console.log("   Panel state:", JSON.stringify(state2, null, 2));

  // 5. Screenshot
  console.log("\n5. Taking screenshot...");
  await cd.take_screenshot({});
  console.log("   Done.");

  // 6. Click a theme
  console.log("\n6. Clicking 'dracula'...");
  const themeClick = await evalJs(`() => {
    const portal = document.getElementById('theme-picker-portal');
    if (!portal) return JSON.stringify({ error: 'no portal' });
    const btn = portal.querySelector('[data-theme-name="dracula"]');
    if (!btn) {
      const all = Array.from(portal.querySelectorAll('[data-theme-name]')).map(e => e.getAttribute('data-theme-name'));
      return JSON.stringify({ error: 'dracula not found', themes: all.slice(0, 10) });
    }
    btn.click();
    return JSON.stringify({
      clicked: true,
      theme: document.documentElement.getAttribute('data-theme'),
      panelDisplay: portal.style.display,
    });
  }`);
  console.log("   Result:", JSON.stringify(themeClick, null, 2));

  // Summary
  console.log("\n" + "=".repeat(50));
  console.log("SUMMARY:");
  console.log("  Portal on body:", state.portalParent === "BODY");
  console.log(
    "  Opens on click:",
    (state2?.display === "flex" || click1?.display === "flex") &&
      (state2?.height > 0 || click1?.height > 0),
  );
  console.log("  Panel height:", state2?.height || click1?.height);
  console.log("  Panel z-index:", state2?.zIndex);
  console.log("  Theme items:", state2?.items || click1?.items);
  console.log("  Theme applied:", themeClick?.theme);
  console.log(
    "  Panel closed after select:",
    themeClick?.panelDisplay === "none",
  );
  console.log("=".repeat(50));
}
