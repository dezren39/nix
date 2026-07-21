await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8000/support-actions/unlock-user" });
// Wait for page load
await new Promise(r => setTimeout(r, 2000));
const snap = await tools.mcp_chrome_devtools.take_snapshot({});
console.log(snap);
