export {};
// Test all three fixes using the correct lootbox chrome devtools API
// API: click({uid}), evaluate_script({function, args}), take_snapshot({})

// Navigate to homepage
await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080" });
await new Promise((r) => setTimeout(r, 2000));

// 1. Check initial URL
const urlCheck = await tools.mcp_chrome_devtools.evaluate_script({
  function:
    "() => JSON.stringify({ url: window.location.href, title: document.title })",
});
console.log("=== INITIAL STATE ===");
console.log(urlCheck.content[0].text);

// 2. Check if theme picker rendered
const pickerCheck = await tools.mcp_chrome_devtools.evaluate_script({
  function:
    "() => { const c = document.getElementById('theme-picker-container'); return c ? 'exists, html: ' + c.innerHTML.substring(0, 200) : 'NOT FOUND'; }",
});
console.log("\n=== THEME PICKER ===");
console.log(pickerCheck.content[0].text);

// 3. Try clicking theme picker button via JS
const pickerClick = await tools.mcp_chrome_devtools.evaluate_script({
  function:
    "() => { const btn = document.querySelector('#theme-picker-container button'); if (!btn) return 'no button found'; btn.click(); const dd = document.querySelector('#theme-picker-container .dropdown'); return 'clicked, dropdown-open: ' + (dd ? dd.classList.contains('dropdown-open') : 'no dropdown') + ', panel display: ' + (document.querySelector('#theme-picker-container .dropdown-content')?.style.display || 'unknown'); }",
});
console.log("Theme picker click:", pickerClick.content[0].text);

// 4. Take screenshot of theme picker open
await new Promise((r) => setTimeout(r, 500));
const r1 = await tools.mcp_chrome_devtools.take_screenshot({});
const img1 = r1.content.find((c: any) => c.type === "image");
console.log(
  "\nScreenshot 1 (theme picker open):",
  img1 ? "captured" : "FAILED",
);

// 5. Close theme picker, then test nav
const closeResult = await tools.mcp_chrome_devtools.evaluate_script({
  function: "() => { document.body.click(); return 'closed'; }",
});

// 6. Click on IRM > Reports via JS to test nav
await new Promise((r) => setTimeout(r, 500));
const navClick = await tools.mcp_chrome_devtools.evaluate_script({
  function:
    "() => { const link = document.querySelector('a[href=\"/irm/reports\"]'); if (!link) return 'no reports link found'; link.click(); return 'clicked reports link'; }",
});
console.log("\n=== NAV TEST ===");
console.log(navClick.content[0].text);

// Wait for SSE fragment to load
await new Promise((r) => setTimeout(r, 3000));

const urlAfter = await tools.mcp_chrome_devtools.evaluate_script({
  function:
    "() => JSON.stringify({ url: window.location.href, title: document.title, mainContent: document.getElementById('main-content')?.textContent?.substring(0, 150) })",
});
console.log("After nav:", urlAfter.content[0].text);

// 7. Take screenshot after nav
const r2 = await tools.mcp_chrome_devtools.take_screenshot({});
const img2 = r2.content.find((c: any) => c.type === "image");
console.log("\nScreenshot 2 (after nav):", img2 ? "captured" : "FAILED");

// 8. Test generate report button
const reportTest = await tools.mcp_chrome_devtools.evaluate_script({
  function:
    "() => { const btn = document.querySelector('button.btn-primary'); return btn ? 'found: ' + btn.textContent : 'no button found'; }",
});
console.log("\n=== REPORT BUTTON ===");
console.log(reportTest.content[0].text);

// 9. Check console errors
const msgs = await tools.mcp_chrome_devtools.list_console_messages({});
console.log("\n=== CONSOLE ===");
console.log(msgs.content[0].text);
