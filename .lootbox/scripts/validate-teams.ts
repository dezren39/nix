/**
 * validate-teams.ts — Validate IRM Teams page with AG Grid and readiness columns
 * Runs in < 10s by using direct navigation instead of waiting for SSE
 */

// Step 1: Navigate directly to teams page
console.log("=== IRM Teams Page Validation ===");
console.log("1. Navigating to /irm/teams...");
await tools.mcp_chrome_devtools.navigate_page({
  url: "http://localhost:8080/irm/teams",
});
console.log("   Navigated");

// Step 2: Wait for AG Grid
await tools.mcp_chrome_devtools.wait_for({
  text: ["ag-grid", "Team"],
  timeout: 5000,
});
console.log("2. Content loaded");

// Step 3: Screenshot
await tools.mcp_chrome_devtools.take_screenshot({});
console.log("3. Screenshot captured");

// Step 4: Snapshot
const snap = await tools.mcp_chrome_devtools.take_snapshot({});
const snapText = snap?.content?.[0]?.text || "";
console.log("4. Snapshot:", snapText.substring(0, 1500));
