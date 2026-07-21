await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/bulk-pages/V3M-LG" });
await new Promise(r => setTimeout(r, 3000));
const result = await tools.mcp_chrome_devtools.evaluate_script({
  "function": "() => { return document.getElementById('bp-grid')?.outerHTML.substring(0, 200); }"
});
console.log(JSON.stringify(result, null, 2));
