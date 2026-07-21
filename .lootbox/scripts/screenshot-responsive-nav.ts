/**
 * Takes screenshots at multiple viewport widths to show responsive nav behavior.
 * Captures both normal view and dropdown-open view at each width.
 *
 * @example lootbox screenshot-responsive-nav.ts
 */

const cd = tools.mcp_chrome_devtools;
const DIR =
  "/Users/drewry.pope/git/incident-response-management/features/screenshots/devtools/2026-04-05_theme-dropdown-overflow";
const APP = "http://localhost:8080";

function text(r: { content: Array<{ type: string; text?: string }> }): string {
  return r.content.map((c) => c.text ?? "").join("");
}

async function evalJs(fn: string): Promise<unknown> {
  const r = await cd.evaluate_script({ function: fn });
  const raw = text(r);
  const m = raw.match(/```(?:json)?\n([\s\S]*?)\n```/);
  try {
    return JSON.parse(m ? m[1] : raw);
  } catch {
    return raw;
  }
}

const widths = [1280, 900, 640, 500, 360];
const results: string[] = [];

for (const w of widths) {
  // Set viewport via CDP emulation
  await evalJs(`() => {
    // We can't resize the window from page JS, but we can report current size
    return { innerWidth: window.innerWidth, innerHeight: window.innerHeight };
  }`);

  // Use CDP to set device metrics
  await cd.evaluate_script({
    function: `() => {
      document.documentElement.style.width = '${w}px';
      document.body.style.width = '${w}px';
      return true;
    }`,
  });

  // Navigate fresh at this size
  await cd.navigate_page({ url: APP });
  await cd.wait_for({ text: ["Theme"], timeout: 5000 });

  // Take nav screenshot
  const navShot = await cd.take_screenshot({
    filePath: `${DIR}/nav-${w}px.png`,
  });
  results.push(`nav-${w}px.png: ${text(navShot).split("\n")[0]}`);

  // Open dropdown and screenshot
  const snap = await cd.take_snapshot({});
  const snapText = text(snap);
  const labelMatch = snapText.match(/uid=(\S+)\s+LabelText/);
  if (labelMatch) {
    await cd.click({ uid: labelMatch[1] });
    await new Promise((r) => setTimeout(r, 400));
    const ddShot = await cd.take_screenshot({
      filePath: `${DIR}/nav-${w}px-dropdown.png`,
    });
    results.push(`nav-${w}px-dropdown.png: ${text(ddShot).split("\n")[0]}`);
    // Close dropdown
    await cd.evaluate_script({
      function: `() => { document.activeElement?.blur(); return true; }`,
    });
  }
}

console.log("Screenshots taken:");
results.forEach((r) => console.log(`  ${r}`));
console.log(`\nSaved to: ${DIR}`);
