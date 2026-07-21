// val-members.ts — Validate IRM Members page
// Uses (tools as any) to avoid Deno type-check slowdown

const cd = (tools as any).mcp_chrome_devtools;

function extractText(r: any): string {
  if (!r?.content) return "";
  return r.content.map((c: any) => c.text ?? "").join("");
}

async function evalJs(fn: string): Promise<any> {
  const r = await cd.evaluate_script({ function: fn });
  const raw = extractText(r);
  const m = raw.match(/```(?:json)?\n([\s\S]*?)\n```/);
  try {
    return JSON.parse(m ? m[1] : raw);
  } catch {
    return m ? m[1] : raw;
  }
}

console.log("=== IRM Members Page Validation ===\n");

// 1. Navigate
console.log("1. Navigating to http://localhost:8080/irm/members ...");
await cd.navigate_page({ url: "http://localhost:8080/irm/members" });
console.log("   Navigation complete");

// 2. Take snapshot
console.log("\n2. Taking DOM snapshot...");
const snapshot = await cd.take_snapshot({});
const snapText = extractText(snapshot);
console.log("   Snapshot length:", snapText.length, "chars");
console.log("   Snapshot preview:\n", snapText.substring(0, 2000));

// 3. Check for AG Grid or table
console.log("\n3. Checking for AG Grid or table...");
const gridCheck = await evalJs(`function() {
  var agRoot = document.querySelector('.ag-root-wrapper, .ag-root, [class*="ag-theme"]');
  var agHeaders = document.querySelectorAll('.ag-header-cell');
  var agRows = document.querySelectorAll('.ag-row');
  var table = document.querySelector('table');
  var tableHeaders = table ? Array.from(table.querySelectorAll('th')).map(function(th) { return th.textContent.trim(); }) : [];
  var tableRows = table ? table.querySelectorAll('tbody tr').length : 0;
  var headerTexts = [];
  agHeaders.forEach(function(h) {
    var text = h.textContent.trim();
    if (text) headerTexts.push(text);
  });
  return JSON.stringify({
    agGridFound: !!agRoot,
    agGridClass: agRoot ? agRoot.className : null,
    agHeaderCount: agHeaders.length,
    agRowCount: agRows.length,
    agColumnHeaders: headerTexts,
    htmlTableFound: !!table,
    htmlTableHeaders: tableHeaders,
    htmlTableRowCount: tableRows,
  });
}`);
console.log("   Grid/table status:", JSON.stringify(gridCheck, null, 2));

// 4. Check all visible columns
console.log("\n4. Listing all visible columns...");
const colCheck = await evalJs(`function() {
  var agHeaders = Array.from(document.querySelectorAll('.ag-header-cell'));
  var agCols = agHeaders.map(function(h) { return h.textContent.trim(); }).filter(Boolean);
  var thEls = Array.from(document.querySelectorAll('table th'));
  var thCols = thEls.map(function(h) { return h.textContent.trim(); }).filter(Boolean);
  var allHeaders = agCols.length > 0 ? agCols : thCols;
  return JSON.stringify({
    source: agCols.length > 0 ? 'ag-grid' : (thCols.length > 0 ? 'html-table' : 'none'),
    columns: allHeaders,
    columnCount: allHeaders.length,
  });
}`);
console.log("   Columns:", JSON.stringify(colCheck, null, 2));

// 5. Check readiness enrichment columns
console.log("\n5. Checking readiness enrichment columns...");
const readinessCheck = await evalJs(`function() {
  var allHeaderEls = Array.from(document.querySelectorAll('.ag-header-cell, table th'));
  var headerTexts = allHeaderEls.map(function(h) { return h.textContent.trim(); });
  var targets = ['Value Stream', 'Readiness', 'Response', 'Manager'];
  var found = {};
  var missing = [];
  for (var i = 0; i < targets.length; i++) {
    var col = targets[i];
    var match = headerTexts.find(function(h) {
      return h.toLowerCase().includes(col.toLowerCase());
    });
    if (match) { found[col] = match; } else { missing.push(col); }
  }
  return JSON.stringify({
    allHeaders: headerTexts,
    readinessColumnsFound: found,
    readinessColumnsMissing: missing,
    allReadinessPresent: missing.length === 0,
  });
}`);
console.log("   Readiness columns:", JSON.stringify(readinessCheck, null, 2));

// 6. Check total member count
console.log("\n6. Checking total member count...");
const countCheck = await evalJs(`function() {
  var agRows = document.querySelectorAll('.ag-row');
  var tableRows = document.querySelectorAll('table tbody tr');
  var rowCount = agRows.length > 0 ? agRows.length : tableRows.length;
  var bodyText = document.body.innerText;
  var countMatches = bodyText.match(/(\\d+)\\s*(?:members?|total|results?|rows?)/i);
  var displayedCount = countMatches ? countMatches[0] : null;
  var statusBar = document.querySelector('.ag-status-bar, .ag-paging-panel');
  var statusText = statusBar ? statusBar.textContent.trim() : null;
  return JSON.stringify({
    agGridRowCount: agRows.length,
    htmlTableRowCount: tableRows.length,
    effectiveRowCount: rowCount,
    displayedCountText: displayedCount,
    statusBarText: statusText,
  });
}`);
console.log("   Count check:", JSON.stringify(countCheck, null, 2));

// 7. Sample row data
console.log("\n7. Sampling row data...");
const sampleRows = await evalJs(`function() {
  var rows = document.querySelectorAll('.ag-row');
  if (rows.length === 0) rows = document.querySelectorAll('table tbody tr');
  var samples = [];
  var count = Math.min(rows.length, 5);
  for (var i = 0; i < count; i++) {
    var cells = rows[i].querySelectorAll('.ag-cell, td');
    var data = [];
    cells.forEach(function(c) { data.push(c.textContent.trim().substring(0, 100)); });
    samples.push(data);
  }
  var noRows = document.querySelector('.ag-overlay-no-rows-wrapper');
  return JSON.stringify({
    totalDataRows: rows.length,
    noRowsOverlay: noRows ? noRows.textContent.trim() : null,
    sampleRows: samples,
  });
}`);
console.log("   Sample rows:", JSON.stringify(sampleRows, null, 2));

// 8. Check console errors
console.log("\n8. Checking console errors...");
const consoleMessages = await cd.list_console_messages({
  types: ["error", "warn"],
});
const consoleTxt = extractText(consoleMessages);
console.log("   Console errors/warnings:\n", consoleTxt.substring(0, 2000));

// 9. Screenshot
console.log("\n9. Taking screenshot...");
await cd.take_screenshot({});
console.log("   Screenshot captured.");

// SUMMARY
console.log("\n" + "=".repeat(60));
console.log("=== VALIDATION SUMMARY ===");
console.log("=".repeat(60));

const hasGrid = gridCheck?.agGridFound || gridCheck?.htmlTableFound;
console.log("AG Grid found:", gridCheck?.agGridFound ?? "UNKNOWN");
console.log("HTML Table found:", gridCheck?.htmlTableFound ?? "UNKNOWN");
console.log("Visible columns:", JSON.stringify(colCheck?.columns));
console.log("Column count:", colCheck?.columnCount ?? "UNKNOWN");
console.log(
  "Readiness columns all present:",
  readinessCheck?.allReadinessPresent ?? "UNKNOWN",
);
console.log("  Found:", JSON.stringify(readinessCheck?.readinessColumnsFound));
console.log(
  "  Missing:",
  JSON.stringify(readinessCheck?.readinessColumnsMissing),
);

const effectiveCount = countCheck?.effectiveRowCount ?? 0;
const sampleCount = sampleRows?.totalDataRows ?? 0;
const totalMembers = Math.max(effectiveCount, sampleCount);
console.log("Total member rows:", totalMembers);
console.log(
  "CRITICAL CHECK - Members NOT zero:",
  totalMembers > 0 ? "PASS" : "*** FAIL ***",
);
console.log("Console errors:", consoleTxt.includes("error") ? "YES" : "NO");
console.log("=".repeat(60));
