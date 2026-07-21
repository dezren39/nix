const result = await tools.mcp_chrome_devtools.evaluate_script({
  "function": "() => { const grid = document.getElementById('bp-grid'); return JSON.stringify({ classes: grid?.className, hasAgRoot: !!grid?.querySelector('.ag-root-wrapper'), bg: grid ? getComputedStyle(grid).backgroundColor : 'N/A', agBg: grid ? getComputedStyle(grid).getPropertyValue('--ag-background-color') : 'N/A', themeOpt: grid?.querySelector('.ag-root-wrapper')?.getAttribute('class') }); }"
});
console.log(JSON.stringify(result, null, 2));
