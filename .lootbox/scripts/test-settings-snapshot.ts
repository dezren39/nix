// Test 2: Take a snapshot of the settings page DOM to verify TzUtil elements
const snap = await tools.mcp_chrome_devtools.take_snapshot({});
console.log("=== Settings page DOM snapshot ===");
console.log(JSON.stringify(snap, null, 2).substring(0, 5000));
