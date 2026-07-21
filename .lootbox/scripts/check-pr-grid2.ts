await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/support-actions/edit-vod-customer-config" });
await new Promise(r => setTimeout(r, 3000));

const result = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { var toggle = document.getElementById('pr-summary-toggle'); if (toggle) { toggle.checked = true; toggle.dispatchEvent(new Event('change')); } var grid = document.getElementById('vc-pr-detail-grid'); var collapse = grid ? grid.closest('.collapse') : null; return JSON.stringify({ collapseH: collapse ? collapse.offsetHeight : 0, collapseStyle: collapse ? collapse.style.cssText : '', gridH: grid ? grid.offsetHeight : 0, gridHTML: grid ? grid.innerHTML.substring(0, 200) : 'EMPTY', gridRows: collapse ? getComputedStyle(collapse).gridTemplateRows : '' }); }" });

console.log(JSON.stringify(result, null, 2));
