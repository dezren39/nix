await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/support-actions/edit-vod-customer-config" });
await new Promise(r => setTimeout(r, 3000));

const result = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { var toggle = document.getElementById('pr-summary-toggle'); if (toggle) { toggle.checked = true; toggle.dispatchEvent(new Event('change')); } var wrapper = document.getElementById('vc-pr-grid-wrapper'); var grid = document.getElementById('vc-pr-detail-grid'); var collapse = grid ? grid.closest('.collapse') : null; var content = grid ? grid.closest('.collapse-content') : null; return JSON.stringify({ wrapperExists: !!wrapper, wrapperH: wrapper ? wrapper.offsetHeight : 0, gridExists: !!grid, gridH: grid ? grid.offsetHeight : 0, gridHTML: grid ? grid.innerHTML.substring(0, 500) : 'EMPTY', collapseH: collapse ? collapse.offsetHeight : 0, collapseStyle: collapse ? collapse.style.cssText : '', gridRows: collapse ? getComputedStyle(collapse).gridTemplateRows : '', contentH: content ? content.offsetHeight : 0 }); }" });

console.log(JSON.stringify(result, null, 2));
