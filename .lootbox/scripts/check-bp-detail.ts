await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/bulk-pages/V3M-LG" });
await new Promise(r => setTimeout(r, 3000));
const snap = await tools.mcp_chrome_devtools.take_snapshot({});
console.log(snap);
