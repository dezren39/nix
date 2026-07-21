// deno-lint-ignore-file no-explicit-any
/**
 * verify-themes-3.ts — Phase 19.6.6 & 19.6.7: Persistence + Mode Toggle
 * @example lootbox verify-themes-3.ts
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

// --- 19.6.6: Persistence ---
console.log("=== 19.6.6: Persistence ===");

await evalJs("() => window.ThemeUtil.setTheme('evertide')");
await new Promise((r) => setTimeout(r, 300));

const [ls, idb, getTheme] = await Promise.all([
  evalJs(`() => ({
    'op-theme': localStorage.getItem('op-theme'),
    'op-mode': localStorage.getItem('op-mode'),
    'op-theme-light': localStorage.getItem('op-theme-light'),
    'op-theme-dark': localStorage.getItem('op-theme-dark'),
  })`),
  evalJs(`() => new Promise(function(resolve) {
    var req = indexedDB.open('op', 1);
    req.onsuccess = function() {
      var db = req.result;
      try {
        var tx = db.transaction('preferences', 'readonly');
        var store = tx.objectStore('preferences');
        var keys = ['theme','mode','theme-dark','theme-light'];
        var out = {}; var done = 0;
        keys.forEach(function(k) {
          var g = store.get(k);
          g.onsuccess = function() { out[k] = g.result; done++; if (done===keys.length) resolve(out); };
          g.onerror = function() { out[k] = null; done++; if (done===keys.length) resolve(out); };
        });
      } catch(e) { resolve({error: e.message}); }
    };
    req.onerror = function() { resolve({error:'db fail'}); };
  })`),
  evalJs("() => window.ThemeUtil.getTheme()"),
]);

console.log("localStorage:", JSON.stringify(ls));
console.log("IndexedDB:", JSON.stringify(idb));
console.log("getTheme():", getTheme);

console.log("PASS localStorage op-theme:", ls && ls["op-theme"] === "evertide");
console.log("PASS localStorage op-mode:", ls && ls["op-mode"] === "dark");
console.log("PASS IndexedDB theme:", idb && idb.theme === "evertide");
console.log("PASS getTheme persisted:", getTheme === "evertide");

// --- 19.6.7: Mode Toggle ---
console.log("\n=== 19.6.7: Mode Toggle ===");

const modeBefore = await evalJs("() => window.ThemeUtil.getMode()");
console.log("Mode before toggle:", modeBefore);

await evalJs("() => window.ThemeUtil.toggleMode()");
await new Promise((r) => setTimeout(r, 300));

const [modeAfter, themeAfter, matchesMode] = await Promise.all([
  evalJs("() => window.ThemeUtil.getMode()"),
  evalJs("() => document.documentElement.getAttribute('data-theme')"),
  evalJs(
    "() => { var t=document.documentElement.getAttribute('data-theme'); var m=window.ThemeUtil.getMode(); return m==='light' ? window.ThemeUtil.isLightTheme(t) : window.ThemeUtil.isDarkTheme(t); }",
  ),
]);
console.log("Mode after toggle:", modeAfter, "theme:", themeAfter);
console.log("PASS mode flipped:", modeBefore !== modeAfter);
console.log("PASS theme matches mode:", matchesMode);

await cd.take_screenshot({});
console.log("Screenshot (after toggle): captured");

// Toggle back
await evalJs("() => window.ThemeUtil.toggleMode()");
await new Promise((r) => setTimeout(r, 300));

const modeBack = await evalJs("() => window.ThemeUtil.getMode()");
console.log("Mode after 2nd toggle:", modeBack);
console.log("PASS toggles back:", modeBack === modeBefore);

// Console errors
const msgs = await cd.list_console_messages({ types: ["error"] });
const raw = text(msgs);
const clean = raw.includes("No console messages") || !raw.includes("error");
console.log("Console errors:", clean ? "none" : raw.slice(0, 200));
console.log("PASS no-console-errors:", clean);

console.log("\n=== Phase 19.6.6 & 19.6.7 COMPLETE ===");
