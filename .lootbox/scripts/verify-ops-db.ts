/**
 * verify-ops-db.ts — Browser verification for centralized OpsDB
 */

await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/" });
await tools.mcp_chrome_devtools.wait_for({ text: ["Operations Portal"], timeout: 5000 });

async function evalJs(code: string): Promise<string> {
  const result = await tools.mcp_chrome_devtools.evaluate_script({
    "function": `() => { ${code} }`,
  });
  const text = result?.content?.find((c: any) => c.type === "text")?.text || JSON.stringify(result);
  return text;
}

const c1 = await evalJs(`return JSON.stringify(window.__OPS_OPS__ || null)`);
console.log("1. window.__OPS_OPS__:", c1);

const c2 = await evalJs(`return JSON.stringify({
  exists: !!window.OpsDB,
  hasStore: typeof window.OpsDB?.store === 'function',
  hasOpen: typeof window.OpsDB?.open === 'function',
  hasConfig: !!window.OpsDB?.config
})`);
console.log("2. window.OpsDB:", c2);

const c3 = await evalJs(`return JSON.stringify(window.OpsDB?.config || null)`);
console.log("3. OpsDB.config:", c3);

const c4 = await evalJs(`
  const prefs = window.OpsDB.store("preferences");
  return prefs.set("__test__", "verify-ok")
    .then(() => prefs.get("__test__"))
    .then(val => { prefs.delete("__test__"); return JSON.stringify({ ok: true, value: val }); })
`);
console.log("4. OpsDB.store('preferences'):", c4);

const c5 = await evalJs(`return JSON.stringify({
  exists: !!window.TzUtil,
  hasFunctions: !!(window.TzUtil?.getTimezone && window.TzUtil?.setTimezone && window.TzUtil?.formatDate),
  currentTz: window.TzUtil?.getTimezone?.() || null,
  pref: window.TzUtil?.getTimezonePref?.() || null,
})`);
console.log("5. window.TzUtil:", c5);

const c6 = await evalJs(`return JSON.stringify({
  exists: !!window.gridCache,
  hasGet: typeof window.gridCache?.get === 'function',
  hasSet: typeof window.gridCache?.set === 'function',
  hasClear: typeof window.gridCache?.clear === 'function',
})`);
console.log("6. window.gridCache:", c6);

const c7 = await evalJs(`return JSON.stringify({ idbClassExists: typeof window.IDB === 'function' })`);
console.log("7. window.IDB class:", c7);

const c8 = await evalJs(`return JSON.stringify({
  debugExists: typeof window.debug === 'function',
  debugNs: localStorage.getItem('ops-debug') || localStorage.getItem('debug') || 'not set',
})`);
console.log("8. debug namespaces:", c8);

const c9 = await evalJs(`return JSON.stringify({
  'ops-theme': localStorage.getItem('ops-theme'),
  'ops-tz': localStorage.getItem('ops-tz'),
  'ops-debug': localStorage.getItem('ops-debug'),
  'legacy_op-theme': localStorage.getItem('op-theme'),
  'legacy_op-tz': localStorage.getItem('op-tz'),
})`);
console.log("9. localStorage keys:", c9);

const c10 = await evalJs(`return JSON.stringify({
  scripts: Array.from(document.querySelectorAll('script[src]')).map(s => s.getAttribute('src')).filter(s => s && (s.includes('dist/') || s.includes('js/'))),
})`);
console.log("10. loaded scripts:", c10);

console.log("\n=== Verification complete ===");
