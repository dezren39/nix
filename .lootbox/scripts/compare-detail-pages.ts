// Navigate to old workflow detail page
await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:1826/support-actions/workflows/runs/wfr-72b91f2c0b9e" });
await new Promise(r => setTimeout(r, 2000));
const oldSnap = await tools.mcp_chrome_devtools.take_screenshot({});
console.log("=== OLD workflow_run_detail page screenshot ===");
console.log(JSON.stringify(oldSnap, null, 2));
