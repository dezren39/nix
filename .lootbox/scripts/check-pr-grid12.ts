await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/support-actions/edit-vod-customer-config?_t=" + Date.now() });
await new Promise(r => setTimeout(r, 4000));

// The checkbox is already checked (collapsed=true means checked=true in DaisyUI collapse?)
// Let's check: is checked = open or checked = closed?
var r = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { var key = 'vod-config-pr-summary'; var reg = window.__resizePersist.registry[key]; var el = reg.el; var cb = el.querySelector(':scope > input[type=checkbox]'); var content = el.querySelector('.collapse-content'); var contentH = content ? content.offsetHeight : -1; return JSON.stringify({ checked: cb.checked, elH: el.offsetHeight, contentH: contentH, contentDisplay: content ? getComputedStyle(content).display : 'N/A', contentVisibility: content ? getComputedStyle(content).visibility : 'N/A', contentMaxH: content ? getComputedStyle(content).maxHeight : 'N/A' }); }" });
console.log(JSON.stringify(r, null, 2));
