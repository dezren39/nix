/**
 * validate-members.ts — Validate IRM members page with AG Grid and readiness columns
 *
 * Steps:
 * 1. Navigate to home page
 * 2. Click "Members" link in sidebar to trigger SSE fragment nav
 * 3. Wait for AG Grid to initialize
 * 4. Take screenshot + snapshot
 * 5. Check AG Grid rendered, column headers, data rows, console errors
 */

const cd = tools.mcp_chrome_devtools;

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

// Step 1: Navigate to home
console.log("1. Navigating to http://localhost:8080/ ...");
await cd.navigate_page({ url: "http://localhost:8080/" });
await new Promise((r) => setTimeout(r, 2000));

const homeCheck = await evalJs(`function() {
  return JSON.stringify({
    url: window.location.pathname,
    title: document.title,
    hasSidebar: !!document.querySelector('nav, [class*="sidebar"], aside'),
  });
}`);
console.log("   Home loaded:", JSON.stringify(homeCheck));

// Step 2: Click "Members" link in sidebar
console.log("\n2. Clicking 'Members' link in sidebar...");
const clickResult = await evalJs(`function() {
  // Find all links, look for one with "members" in href or text
  var links = Array.from(document.querySelectorAll('a'));
  var membersLink = null;
  for (var i = 0; i < links.length; i++) {
    var href = links[i].getAttribute('href') || '';
    var text = links[i].textContent || '';
    if (href.includes('/irm/members') || (text.toLowerCase().includes('members') && href.includes('/irm'))) {
      membersLink = links[i];
      break;
    }
  }
  if (!membersLink) {
    // Try data-on-click attribute (Datastar SSE pattern)
    var dsLinks = document.querySelectorAll('[data-on-click__prevent]');
    for (var j = 0; j < dsLinks.length; j++) {
      var attr = dsLinks[j].getAttribute('data-on-click__prevent') || '';
      if (attr.includes('members')) {
        membersLink = dsLinks[j];
        break;
      }
    }
  }
  if (!membersLink) {
    return JSON.stringify({ error: 'Members link not found', allLinks: links.map(function(l) { return { href: l.href, text: l.textContent.trim().substring(0, 50) }; }).slice(0, 20) });
  }
  membersLink.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, view: window }));
  return JSON.stringify({ clicked: true, href: membersLink.href, text: membersLink.textContent.trim() });
}`);
console.log("   Click result:", JSON.stringify(clickResult));

// Step 3: Wait for AG Grid to initialize
console.log("\n3. Waiting 3 seconds for AG Grid to initialize...");
await new Promise((r) => setTimeout(r, 3000));

// Check URL after navigation
const navState = await evalJs(`function() {
  return JSON.stringify({
    url: window.location.pathname + window.location.search,
    title: document.title,
  });
}`);
console.log("   Navigation state:", JSON.stringify(navState));

// Step 4: Take screenshot
console.log("\n4. Taking screenshot...");
await cd.take_screenshot({});
console.log("   Screenshot captured.");

// Step 5: Take snapshot (DOM)
console.log("\n5. Taking DOM snapshot...");
const snapshot = await cd.take_snapshot({});
const snapText = extractText(snapshot);
console.log("   Snapshot length:", snapText.length, "chars");

// Step 6: Validate AG Grid rendered
console.log("\n6. Checking AG Grid...");
const gridCheck = await evalJs(`function() {
  var agRoot = document.querySelector('.ag-root-wrapper, .ag-root, [class*="ag-theme"]');
  var agHeaders = document.querySelectorAll('.ag-header-cell');
  var agRows = document.querySelectorAll('.ag-row');
  var agCells = document.querySelectorAll('.ag-cell');
  var headerTexts = [];
  agHeaders.forEach(function(h) {
    var text = h.textContent.trim();
    if (text) headerTexts.push(text);
  });
  return JSON.stringify({
    agGridFound: !!agRoot,
    agGridClass: agRoot ? agRoot.className : null,
    headerCount: agHeaders.length,
    rowCount: agRows.length,
    cellCount: agCells.length,
    columnHeaders: headerTexts,
  });
}`);
console.log("   AG Grid status:", JSON.stringify(gridCheck));

// Step 7: Check for readiness columns specifically
console.log("\n7. Checking readiness columns...");
const readinessCheck = await evalJs(`function() {
  var headers = Array.from(document.querySelectorAll('.ag-header-cell'));
  var headerTexts = headers.map(function(h) { return h.textContent.trim(); });
  
  var targetColumns = ['Value Stream', 'Readiness', 'Ack Time', 'Manager'];
  var found = {};
  var missing = [];
  
  for (var i = 0; i < targetColumns.length; i++) {
    var col = targetColumns[i];
    var match = headerTexts.find(function(h) {
      return h.toLowerCase().includes(col.toLowerCase());
    });
    if (match) {
      found[col] = match;
    } else {
      missing.push(col);
    }
  }
  
  return JSON.stringify({
    allHeaders: headerTexts,
    readinessColumnsFound: found,
    readinessColumnsMissing: missing,
    allReadinessPresent: missing.length === 0,
  });
}`);
console.log("   Readiness columns:", JSON.stringify(readinessCheck));

// Step 8: Check existing columns still work
console.log("\n8. Checking existing/standard columns...");
const existingColCheck = await evalJs(`function() {
  var headers = Array.from(document.querySelectorAll('.ag-header-cell'));
  var headerTexts = headers.map(function(h) { return h.textContent.trim(); });
  
  // Common member columns that should exist
  var expectedCols = ['Name', 'Email', 'Role', 'Status', 'Team'];
  var found = {};
  var missing = [];
  
  for (var i = 0; i < expectedCols.length; i++) {
    var col = expectedCols[i];
    var match = headerTexts.find(function(h) {
      return h.toLowerCase().includes(col.toLowerCase());
    });
    if (match) {
      found[col] = match;
    } else {
      missing.push(col);
    }
  }
  
  return JSON.stringify({
    existingColumnsFound: found,
    existingColumnsMissing: missing,
  });
}`);
console.log("   Existing columns:", JSON.stringify(existingColCheck));

// Step 9: Check data rows are populated
console.log("\n9. Checking data rows...");
const rowDataCheck = await evalJs(`function() {
  var rows = document.querySelectorAll('.ag-row');
  var sampleData = [];
  var count = Math.min(rows.length, 3);
  for (var i = 0; i < count; i++) {
    var cells = rows[i].querySelectorAll('.ag-cell');
    var cellTexts = [];
    cells.forEach(function(c) {
      cellTexts.push(c.textContent.trim().substring(0, 80));
    });
    sampleData.push(cellTexts);
  }
  
  // Check for "No Rows" overlay
  var noRows = document.querySelector('.ag-overlay-no-rows-wrapper');
  var noRowsText = noRows ? noRows.textContent.trim() : null;
  
  return JSON.stringify({
    totalRows: rows.length,
    hasData: rows.length > 0,
    noRowsOverlay: noRowsText,
    sampleRows: sampleData,
  });
}`);
console.log("   Row data:", JSON.stringify(rowDataCheck));

// Step 10: Check console errors
console.log("\n10. Checking console errors...");
const consoleMessages = await cd.list_console_messages({
  types: ["error", "warning"],
});
const consoleTxt = extractText(consoleMessages);
console.log("   Console errors/warnings:", consoleTxt.substring(0, 1500));

// Summary
console.log("\n=== VALIDATION SUMMARY ===");
console.log("Members page loaded via SSE nav:", JSON.stringify(clickResult));
console.log("AG Grid rendered:", JSON.stringify(gridCheck));
console.log("Readiness columns:", JSON.stringify(readinessCheck));
console.log("Existing columns:", JSON.stringify(existingColCheck));
console.log("Data rows:", JSON.stringify(rowDataCheck));
console.log("=== END ===");
