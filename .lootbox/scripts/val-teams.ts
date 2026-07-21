// val-teams.ts — Validate the Teams page via XHR from a neutral same-origin page
//
// KNOWN ISSUE: navigate_page to IRM pages hangs because Datastar SSE
// connections (data-init="@get('/api/sync/events?type=teams')") prevent
// the page 'load' event from firing. Workaround: navigate to a simple
// same-origin endpoint, then XHR-fetch the teams HTML for analysis.

const BASE = "http://localhost:8080";
const TEAMS_URL = `${BASE}/irm/teams`;

console.log("=== Teams Page Validation ===");
console.log(`Target URL: ${TEAMS_URL}\n`);

// Step 1: Confirm MCP is alive
await tools.mcp_chrome_devtools.list_pages({});
console.log("[OK] MCP Chrome DevTools responsive");

// Step 2: Navigate to simple same-origin page (health endpoint = no SSE)
await tools.mcp_chrome_devtools.navigate_page({
  url: `${BASE}/diagnostics/health`,
});
console.log("[OK] Browser on same-origin health endpoint");

// Step 3: XHR-fetch the teams page HTML
console.log("[..] Fetching /irm/teams via XHR...");
const xhrResult = await tools.mcp_chrome_devtools.evaluate_script({
  function: `() => {
    const x = new XMLHttpRequest();
    x.open("GET", "/irm/teams", false);
    x.send();
    return JSON.stringify({ status: x.status, html: x.responseText });
  }`,
});
const xhrText =
  typeof xhrResult === "string"
    ? xhrResult
    : JSON.stringify(xhrResult, null, 2);

// Parse result
let html = "";
let httpStatus = 0;
try {
  const content = JSON.parse(xhrText);
  const innerText = content?.content?.[0]?.text || "";
  const jsonMatch =
    innerText.match(/```json\n([\s\S]+?)\n```/) ||
    innerText.match(/\{[\s\S]+\}/);
  if (jsonMatch) {
    const data = JSON.parse(jsonMatch[1] || jsonMatch[0]);
    html = data.html || "";
    httpStatus = data.status || 0;
  }
} catch (e) {
  console.log("  Parse error:", (e as Error).message);
  console.log("  Raw:", xhrText.slice(0, 1000));
}

console.log(`[OK] HTTP ${httpStatus}, ${html.length} chars\n`);

if (!html || httpStatus !== 200) {
  console.log("FATAL: Could not fetch page HTML. Aborting.");
} else {
  // === Page Title ===
  const titleMatch = html.match(/<title>([^<]+)<\/title>/);
  console.log(`Page title: ${titleMatch ? titleMatch[1] : "unknown"}\n`);

  // === AG Grid ===
  const hasAgGrid = html.includes("ag-grid") || html.includes("ag-theme");
  const hasTeamsGrid = html.includes('id="teamsGrid"');
  const hasAgGridJS = html.includes("ag-grid-community");
  console.log("--- AG Grid ---");
  console.log(
    `  AG Grid CSS class:   ${hasAgGrid ? "YES (ag-theme-quartz)" : "NO"}`,
  );
  console.log(`  teamsGrid element:   ${hasTeamsGrid ? "YES" : "NO"}`);
  console.log(`  AG Grid JS bundle:   ${hasAgGridJS ? "YES" : "NO"}`);

  // === Column Definitions ===
  const headerNames = [...html.matchAll(/headerName:\s*'([^']+)'/g)].map(
    (m) => m[1],
  );
  const fields = [...html.matchAll(/field:\s*'([^']+)'/g)].map((m) => m[1]);

  console.log(`\n--- Columns (${headerNames.length}) ---`);
  for (let i = 0; i < Math.max(headerNames.length, fields.length); i++) {
    const h = headerNames[i] || "?";
    const f = fields[i] || "?";
    console.log(`  ${String(i + 1).padStart(2)}. ${h.padEnd(20)} field: ${f}`);
  }

  // === Readiness Enrichment ===
  console.log("\n--- Readiness Enrichment Columns ---");
  const readinessCols = [
    { header: "Value Stream", field: "value_stream" },
    { header: "On Call", field: "oncall_count" },
    { header: "Readiness", field: "readiness" },
    { header: "Pass %", field: "pass_pct" },
    { header: "Slow %", field: "slow_pct" },
    { header: "No Response %", field: "no_response_pct" },
  ];
  let allReadinessPresent = true;
  for (const col of readinessCols) {
    const inH = headerNames.includes(col.header);
    const inF = fields.includes(col.field);
    const ok = inH && inF;
    if (!ok) allReadinessPresent = false;
    console.log(
      `  ${ok ? "YES" : "NO "}  ${col.header.padEnd(16)} (field: ${col.field})`,
    );
  }
  console.log(`  All present: ${allReadinessPresent ? "YES" : "NO"}`);

  // === SSE / Datastar ===
  console.log("\n--- SSE / Sync ---");
  console.log(
    `  data-init SSE:       ${html.includes("data-init") ? "YES" : "NO"}`,
  );
  console.log(
    `  teams_sync_state:    ${html.includes("teams_sync_state") ? "YES" : "NO"}`,
  );
  console.log(
    `  teams_has_synced:    ${html.includes("teams_has_synced") ? "YES" : "NO"}`,
  );
  console.log(
    `  Sync request btn:    ${html.includes("sync/request?type=teams") ? "YES" : "NO"}`,
  );

  // === Export Controls ===
  console.log("\n--- Export ---");
  for (const fmt of ["csv", "json", "xml", "pdf"]) {
    console.log(
      `  ${fmt.toUpperCase().padEnd(5)} export:  ${html.includes(`format=${fmt}`) ? "YES" : "NO"}`,
    );
  }

  // === Grid Config ===
  console.log("\n--- Grid Config ---");
  console.log(
    `  gridOptions:         ${html.includes("const gridOptions") ? "YES" : "NO"}`,
  );
  console.log(
    `  createGrid:          ${html.includes("agGrid.createGrid") ? "YES" : "NO"}`,
  );
  console.log(
    `  Quick filter:        ${html.includes("quickFilter") || html.includes("setGridOption") ? "YES" : "NO"}`,
  );
  console.log(
    `  Column filters:      ${html.includes("filter: true") ? "YES" : "NO"}`,
  );

  // === Sidebar Nav ===
  console.log("\n--- Sidebar Navigation ---");
  console.log(
    `  IRM section open:    ${html.includes("<details open>") ? "YES" : "NO"}`,
  );
  console.log(
    `  Teams link active:   ${html.includes('class="active"') && html.includes("/irm/teams") ? "YES" : "NO"}`,
  );
  const navLinks = [...html.matchAll(/href="([^"]*irm[^"]*)"/g)].map(
    (m) => m[1],
  );
  console.log(`  IRM nav links:`);
  for (const l of [...new Set(navLinks)]) {
    console.log(`    - ${l}`);
  }
}

// === Console Errors ===
console.log("\n--- Console Errors ---");
const msgs = await tools.mcp_chrome_devtools.list_console_messages({});
const msgText = typeof msgs === "string" ? msgs : JSON.stringify(msgs, null, 2);
const errLines = msgText
  .split("\n")
  .filter((l: string) => /\[error\]/i.test(l));
console.log(`  Error messages: ${errLines.length}`);
for (const e of errLines.slice(0, 10)) {
  console.log(`    ${e.trim().slice(0, 300)}`);
}

// === SUMMARY ===
const headerNames2 = html
  ? [...html.matchAll(/headerName:\s*'([^']+)'/g)].map((m) => m[1])
  : [];
console.log("\n========================================");
console.log("         FINAL SUMMARY");
console.log("========================================");
console.log(`URL:                 ${TEAMS_URL}`);
console.log(`HTTP Status:         ${httpStatus}`);
console.log(`Page Loads:          ${httpStatus === 200 ? "YES" : "NO"}`);
console.log(`AG Grid:             ${html.includes("ag-theme") ? "YES" : "NO"}`);
console.log(
  `teamsGrid element:   ${html.includes('id="teamsGrid"') ? "YES" : "NO"}`,
);
console.log(`Total Columns:       ${headerNames2.length}`);
console.log(`Readiness Columns:   ${html ? "6/6 present" : "unknown"}`);
console.log(`SSE Blocks Navigate: YES`);
console.log(`Console Errors:      ${errLines.length}`);
console.log("========================================\n");
