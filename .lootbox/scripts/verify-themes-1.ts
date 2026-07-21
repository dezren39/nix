// deno-lint-ignore-file no-explicit-any
/**
 * verify-themes-1.ts — Phase 19.6.4: Custom Theme Rendering
 * @example lootbox verify-themes-1.ts
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
// No sleep — page is already loaded from prior navigation most likely

const initialTheme = await evalJs(
  "() => document.documentElement.getAttribute('data-theme')",
);
console.log("Initial data-theme:", initialTheme);

const tuExists = await evalJs(
  "() => typeof window.ThemeUtil === 'object' && window.ThemeUtil !== null",
);
console.log("ThemeUtil exists:", tuExists);

const tuInfo = await evalJs(`() => {
  if (!window.ThemeUtil) return null;
  return {
    methods: ['setTheme','getTheme','getMode','toggleMode','initTheme'].map(function(m) { return m + ':' + typeof window.ThemeUtil[m]; }),
    allCount: window.ThemeUtil.ALL_THEMES.length,
    featured: window.ThemeUtil.FEATURED_THEMES,
  };
}`);
console.log("ThemeUtil info:", JSON.stringify(tuInfo));

const customs = await evalJs(`() => {
  if (!window.ThemeUtil) return null;
  return ['blueprint','evermoss','evertide'].map(function(t) {
    return { name: t, inAll: window.ThemeUtil.ALL_THEMES.includes(t), light: window.ThemeUtil.isLightTheme(t), dark: window.ThemeUtil.isDarkTheme(t) };
  });
}`);
console.log("Custom themes:", JSON.stringify(customs));

// Apply each custom theme and check CSS vars
const themes = ["blueprint", "evermoss", "evertide"];
for (const t of themes) {
  await evalJs(
    "() => document.documentElement.setAttribute('data-theme', '" + t + "')",
  );
  const [applied, colors] = await Promise.all([
    evalJs("() => document.documentElement.getAttribute('data-theme')"),
    evalJs(
      "() => { var cs = getComputedStyle(document.documentElement); return { primary: cs.getPropertyValue('--color-primary').trim(), base100: cs.getPropertyValue('--color-base-100').trim() }; }",
    ),
  ]);
  console.log(t + ": applied=" + applied + " colors=" + JSON.stringify(colors));
  await cd.take_screenshot({});
  console.log(t + ": screenshot captured");
}

// DaisyUI default
await evalJs(
  "() => document.documentElement.setAttribute('data-theme', 'dracula')",
);
const dracula = await evalJs(
  "() => document.documentElement.getAttribute('data-theme')",
);
console.log("dracula: applied=" + dracula);

console.log("\n=== Phase 19.6.4 COMPLETE ===");
