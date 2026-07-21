await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/users" });
await new Promise(r => setTimeout(r, 3000));
const result = await tools.mcp_chrome_devtools.evaluate_script({
  "function": "() => { const grid = document.getElementById('users-grid'); return JSON.stringify({ classes: grid?.className, agBg: grid ? getComputedStyle(grid).getPropertyValue('--ag-background-color') : 'N/A', agRootBg: grid?.querySelector('.ag-root-wrapper') ? getComputedStyle(grid.querySelector('.ag-root-wrapper')).backgroundColor : 'N/A' }); }"
});
console.log(JSON.stringify(result, null, 2));
