// Test 1: Navigate to settings page, verify timezone UI loads
const nav = await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/irm/settings" });
console.log("=== Navigate to settings ===");
console.log(JSON.stringify(nav, null, 2));

// Take a screenshot of settings page
const screenshot = await tools.mcp_chrome_devtools.take_screenshot({});
console.log("\n=== Settings page screenshot ===");
console.log(JSON.stringify(screenshot, null, 2));
