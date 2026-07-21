await new Promise(r => setTimeout(r, 3000));
const r = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { var gridEl = document.getElementById('vc-pr-detail-grid'); return JSON.stringify({ rowCount: gridEl.querySelectorAll('.ag-row').length, hasApi: !!gridEl.__agGridApi, inner: gridEl.innerHTML.substring(0, 200) }); }" });
console.log(r.content[0].text);
