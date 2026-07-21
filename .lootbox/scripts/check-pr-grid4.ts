await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/support-actions/edit-vod-customer-config?_t=" + Date.now() });
await new Promise(r => setTimeout(r, 4000));

// Expand PR
await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { var t = document.getElementById('pr-summary-toggle'); t.checked = true; t.dispatchEvent(new Event('change')); return 'ok'; }" });
await new Promise(r => setTimeout(r, 500));

var r = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { var c = document.querySelector('[data-resize-key=\"vod-config-pr-summary\"]'); var g = document.getElementById('vc-pr-detail-grid'); return JSON.stringify({ collapseH: c.offsetHeight, style: c.style.cssText, gridH: g.offsetHeight, hasContent: g.innerHTML.length > 0, gridRows: getComputedStyle(c).gridTemplateRows }); }" });
console.log(JSON.stringify(r, null, 2));
