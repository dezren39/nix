await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/support-actions/edit-vod-customer-config?_t=" + Date.now() });
await new Promise(r => setTimeout(r, 5000));

const r = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { var gridEl = document.getElementById('vc-pr-detail-grid'); if (!gridEl) return 'NO GRID EL'; return JSON.stringify({ gridH: gridEl.offsetHeight, hasApi: !!gridEl.__agGridApi, hasAgRoot: !!gridEl.querySelector('.ag-root-wrapper') }); }" });
console.log("PR Grid:", r.content[0].text);

const msgs = await tools.mcp_chrome_devtools.list_console_messages({});
const text = JSON.stringify(msgs);
const errIdx = text.indexOf('[error]');
console.log("Errors:", errIdx > -1 ? text.substring(errIdx, errIdx + 200) : "none");
