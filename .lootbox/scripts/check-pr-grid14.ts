await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/support-actions/edit-vod-customer-config?_t=" + Date.now() });
await new Promise(r => setTimeout(r, 4000));

var r = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { var stored = localStorage.getItem('ops:resize:vod-config-pr-summary'); var collapse = localStorage.getItem('ops:collapse:vod-config-pr-summary'); var el = document.querySelector('[data-resize-key=\"vod-config-pr-summary\"]'); var inlineAttr = el.getAttribute('style'); return JSON.stringify({ storedHeight: stored, storedCollapse: collapse, htmlStyle: inlineAttr }); }" });
console.log(JSON.stringify(r, null, 2));
