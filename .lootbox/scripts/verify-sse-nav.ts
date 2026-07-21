/**
 * verify-sse-nav.ts — Verify Datastar SSE fragment navigation
 */

// Navigate to the home page
const nav = await tools.mcp_chrome_devtools.navigate_page({
  url: "http://localhost:8080/",
});
console.log("1. Navigate to home:", JSON.stringify(nav).substring(0, 200));

// Wait for page content
await tools.mcp_chrome_devtools.wait_for({
  text: ["Portal Home"],
  timeout: 5000,
});
console.log("2. Home page loaded (found 'Portal Home')");

// Take snapshot to see page structure
const snap1 = await tools.mcp_chrome_devtools.take_snapshot({});
const snapText = snap1?.content?.[0]?.text || JSON.stringify(snap1);
console.log("3. Snapshot (first 500 chars):", snapText.substring(0, 500));

// Check Datastar attributes exist via JS eval
const hasDatastar = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() { return document.querySelectorAll('[data-on-click__prevent]').length; }`,
});
console.log(
  "4. Links with data-on-click__prevent:",
  JSON.stringify(hasDatastar),
);

// Check required IDs
const hasIds = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() { return JSON.stringify({
    mainContent: !!document.getElementById('main-content'),
    navSidebar: !!document.getElementById('nav-sidebar'),
  }); }`,
});
console.log("5. Required IDs:", JSON.stringify(hasIds));

// Get current URL
const url1 = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() { return window.location.pathname; }`,
});
console.log("6. Current URL:", JSON.stringify(url1));

// Take screenshot before navigation
await tools.mcp_chrome_devtools.take_screenshot({});
console.log("7. Screenshot before nav taken");

// Get the uid for Admin link by taking snapshot and finding it
// Actually, we need to find the uid. Let's use evaluate_script to click
const clickAdmin = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    var link = document.querySelector('a[href="/admin"][data-on-click__prevent]');
    if (link) { link.click(); return 'clicked admin'; }
    return 'admin link not found';
  }`,
});
console.log("8. Click admin via JS:", JSON.stringify(clickAdmin));

// Wait for content to change
await new Promise((r) => setTimeout(r, 2000));

// Check URL updated
const url2 = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() { return window.location.pathname; }`,
});
console.log("9. URL after admin click:", JSON.stringify(url2));

// Check content changed
const adminContent = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() { 
    var el = document.getElementById('main-content');
    return el ? el.innerHTML.substring(0, 300) : 'no main-content';
  }`,
});
console.log("10. Admin main-content:", JSON.stringify(adminContent));

// Check active state
const activeLinks = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    return JSON.stringify(
      Array.from(document.querySelectorAll('#nav-sidebar a.active')).map(function(a) { return a.textContent.trim(); })
    );
  }`,
});
console.log("11. Active sidebar links:", JSON.stringify(activeLinks));

// Take screenshot of admin
await tools.mcp_chrome_devtools.take_screenshot({});
console.log("12. Admin page screenshot taken");

// Now navigate to IRM Members
const clickMembers = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    var link = document.querySelector('a[href="/irm/members"][data-on-click__prevent]');
    if (link) { link.click(); return 'clicked members'; }
    return 'members link not found';
  }`,
});
console.log("13. Click members:", JSON.stringify(clickMembers));

await new Promise((r) => setTimeout(r, 2000));

const url3 = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() { return window.location.pathname; }`,
});
console.log("14. URL after members click:", JSON.stringify(url3));

const membersContent = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() { 
    var el = document.getElementById('main-content');
    return el ? el.innerHTML.substring(0, 300) : 'no main-content';
  }`,
});
console.log("15. Members content:", JSON.stringify(membersContent));

// Check console for errors
const msgs = await tools.mcp_chrome_devtools.list_console_messages({});
console.log("16. Console messages:", JSON.stringify(msgs).substring(0, 500));

// Final screenshot
await tools.mcp_chrome_devtools.take_screenshot({});
console.log("17. Members page screenshot taken");

console.log("\n=== VERIFICATION SUMMARY ===");
console.log(
  "Check items 6→9→14 for URL progression: / → /admin → /irm/members",
);
console.log("Check items 10, 15 for content changes");
console.log("Check item 11 for active state");
