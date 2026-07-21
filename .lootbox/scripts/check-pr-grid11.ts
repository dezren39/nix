await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/support-actions/edit-vod-customer-config?_t=" + Date.now() });
await new Promise(r => setTimeout(r, 4000));

// Manually test the change handler logic
var r = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { var key = 'vod-config-pr-summary'; var rp = window.__resizePersist; var reg = rp.registry[key]; var el = reg.el; var cb = el.querySelector(':scope > input[type=checkbox]'); var before = { checked: cb.checked, elH: el.offsetHeight, style: el.style.height }; cb.checked = true; cb.dispatchEvent(new Event('change', {bubbles: true})); var after = { checked: cb.checked, elH: el.offsetHeight, style: el.style.height }; return JSON.stringify({before: before, after: after, defaultH: reg.defaultHeight}); }" });
console.log(JSON.stringify(r, null, 2));
