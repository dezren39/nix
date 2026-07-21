await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/support-actions/edit-vod-customer-config?_t=" + Date.now() });
await new Promise(r => setTimeout(r, 4000));

// Check the new elements exist
const r = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { return JSON.stringify({ previewBtn: !!document.getElementById('config-edit-preview-btn'), refreshBtn: !!document.getElementById('config-edit-refresh-diff-btn'), expandBtn: !!document.getElementById('config-edit-expand-toggle'), diffEl: !!document.getElementById('config-edit-diff'), configSection: !!document.getElementById('config-edit-content-section'), refreshHidden: document.getElementById('config-edit-refresh-diff-btn').classList.contains('hidden'), expandHidden: document.getElementById('config-edit-expand-toggle').classList.contains('hidden'), diffResizable: document.getElementById('config-edit-diff').classList.contains('resize-y') }); }" });
console.log("Elements:", r.content[0].text);

// Check for errors
const msgs = await tools.mcp_chrome_devtools.list_console_messages({});
const text = JSON.stringify(msgs);
const errIdx = text.indexOf('[error]');
console.log("Errors:", errIdx > -1 ? text.substring(errIdx, errIdx + 200) : "none");
