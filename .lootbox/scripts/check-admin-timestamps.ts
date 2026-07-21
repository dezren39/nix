await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8083/admin" });
// Wait for page load
await new Promise(r => setTimeout(r, 3000));
const snap = await tools.mcp_chrome_devtools.take_snapshot({});
console.log(snap);
