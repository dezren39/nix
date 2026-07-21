// Login
await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/auth/login" });
let snap = await tools.mcp_chrome_devtools.take_snapshot({});
console.log("Login page snapshot:", JSON.stringify(snap).slice(0, 500));

// Find username field uid from snapshot and fill
await tools.mcp_chrome_devtools.fill({ uid: "1_32", value: "admin" });
await tools.mcp_chrome_devtools.fill({ uid: "1_34", value: "admin" });
await tools.mcp_chrome_devtools.click({ uid: "1_35" });

// Wait for redirect
await tools.mcp_chrome_devtools.wait_for({ text: ["Operations Portal"], timeout: 5000 });

// Go to unlock user
await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/support-actions/unlock-user" });
await tools.mcp_chrome_devtools.wait_for({ text: ["Locked Users"], timeout: 5000 });

const screenshot = await tools.mcp_chrome_devtools.take_screenshot({});
console.log(JSON.stringify(screenshot).slice(0, 100));
