await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/bulk-pages" });
await new Promise(r => setTimeout(r, 3000));
const result = await tools.mcp_chrome_devtools.evaluate_script({
  "function": "() => { document.getElementById('btn-new-bulk-page')?.click(); return 'clicked'; }"
});
console.log("click result:", result);
await new Promise(r => setTimeout(r, 500));
const snap = await tools.mcp_chrome_devtools.take_snapshot({});
console.log(JSON.stringify(snap).substring(0, 2000));
