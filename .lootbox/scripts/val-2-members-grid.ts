// Validate: Members page has AG Grid and readiness columns
// Navigate to /irm/members, check AG Grid, readiness columns, data populated

const cd = tools.mcp_chrome_devtools;
const results: string[] = [];
let passed = true;

function log(msg: string) {
  results.push(msg);
}
function fail(msg: string) {
  results.push(`FAIL: ${msg}`);
  passed = false;
}
function pass(msg: string) {
  results.push(`OK: ${msg}`);
}

try {
  // Step 1: Navigate to /irm/members
  log("--- Step 1: Navigate to /irm/members ---");
  await cd.navigate_page({ url: "http://localhost:8080/irm/members" });
  await new Promise((r) => setTimeout(r, 3000));

  const urlCheck = await cd.evaluate_script({
    function: "() => window.location.href",
  });
  log(`URL: ${urlCheck?.content?.[0]?.text || "unknown"}`);

  // Step 2: Check AG Grid present
  log("--- Step 2: Check AG Grid ---");
  const gridCheck = await cd.evaluate_script({
    function: `() => {
      const root = document.querySelector('.ag-root-wrapper');
      const body = document.querySelector('.ag-body-viewport');
      const header = document.querySelector('.ag-header');
      return JSON.stringify({
        hasRootWrapper: !!root,
        hasBodyViewport: !!body,
        hasHeader: !!header,
        gridElements: document.querySelectorAll('[class*="ag-"]').length
      });
    }`,
  });
  const gridData = JSON.parse(gridCheck?.content?.[0]?.text || "{}");
  log(`AG Grid check: ${JSON.stringify(gridData)}`);

  if (gridData.hasRootWrapper) {
    pass("AG Grid root wrapper found");
  } else if (gridData.gridElements > 0) {
    pass(`AG Grid elements found (${gridData.gridElements} ag-* elements)`);
  } else {
    fail("AG Grid NOT found on members page");
  }

  // Step 3: Check readiness columns
  log("--- Step 3: Check readiness columns ---");
  const columnsCheck = await cd.evaluate_script({
    function: `() => {
      // Get all header cell text
      const headerCells = Array.from(document.querySelectorAll('.ag-header-cell'));
      const colTexts = headerCells.map(cell => {
        const textEl = cell.querySelector('.ag-header-cell-text');
        return textEl?.textContent?.trim() || cell.textContent?.trim() || '';
      }).filter(Boolean);

      // Also check for column IDs
      const colIds = headerCells.map(cell => cell.getAttribute('col-id') || '').filter(Boolean);

      // Check for readiness-related columns
      const readinessTerms = ['value_stream', 'readiness_status', 'readiness', 'response_minutes', 'response', 'status'];
      const foundReadiness = colTexts.filter(t => readinessTerms.some(term => t.toLowerCase().includes(term)));
      const foundReadinessIds = colIds.filter(id => readinessTerms.some(term => id.toLowerCase().includes(term)));

      return JSON.stringify({
        totalColumns: headerCells.length,
        columnTexts: colTexts.slice(0, 20),
        columnIds: colIds.slice(0, 20),
        readinessColumns: foundReadiness,
        readinessColumnIds: foundReadinessIds
      });
    }`,
  });
  const colData = JSON.parse(columnsCheck?.content?.[0]?.text || "{}");
  log(`Columns: ${JSON.stringify(colData, null, 2)}`);

  if (
    colData.readinessColumns?.length > 0 ||
    colData.readinessColumnIds?.length > 0
  ) {
    pass(
      `Readiness columns found: ${[...colData.readinessColumns, ...colData.readinessColumnIds].join(", ")}`,
    );
  } else {
    fail(
      `No readiness columns found. Columns present: ${colData.columnTexts?.join(", ")}`,
    );
  }

  // Step 4: Check data populated
  log("--- Step 4: Check data populated ---");
  const dataCheck = await cd.evaluate_script({
    function: `() => {
      const rows = document.querySelectorAll('.ag-row');
      const centerRows = document.querySelectorAll('.ag-center-cols-container .ag-row');
      // Also check for a "total" or row count indicator
      const totalEl = document.querySelector('[data-total], .ag-paging-description, .ag-status-bar');
      const totalText = totalEl?.textContent?.trim() || '';
      
      // Get first row data as sample
      let sampleRow = null;
      if (rows.length > 0) {
        const cells = Array.from(rows[0].querySelectorAll('.ag-cell'));
        sampleRow = cells.map(c => c.textContent?.trim()).slice(0, 8);
      }

      return JSON.stringify({
        rowCount: rows.length,
        centerRowCount: centerRows.length,
        totalIndicator: totalText,
        sampleFirstRow: sampleRow
      });
    }`,
  });
  const dataData = JSON.parse(dataCheck?.content?.[0]?.text || "{}");
  log(`Data check: ${JSON.stringify(dataData, null, 2)}`);

  if (dataData.rowCount > 0) {
    pass(`Data populated: ${dataData.rowCount} rows found`);
  } else {
    fail("No data rows found - total members is 0");
  }
} catch (err: any) {
  fail(`Script error: ${err.message}`);
}

console.log("\n========================================");
console.log("VALIDATION 2: Members Page AG Grid");
console.log("========================================");
results.forEach((r) => console.log(r));
console.log(`\nRESULT: ${passed ? "PASS" : "FAIL"}`);
console.log("========================================\n");
