await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/support-actions/edit-vod-customer-config?_t=" + Date.now() });
await new Promise(r => setTimeout(r, 4000));

var r = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { var key = 'vod-config-pr-summary'; var reg = window.__resizePersist.registry[key]; var el = reg.el; var cb = el.querySelector(':scope > input[type=checkbox]'); var stored = localStorage.getItem('ops:resize:vod-config-pr-summary'); return JSON.stringify({ checked: cb.checked, elH: el.offsetHeight, style: el.style.height, defaultH: reg.defaultHeight, stored: stored }); }" });
console.log(JSON.stringify(r, null, 2));
