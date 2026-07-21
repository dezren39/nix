await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/support-actions/edit-vod-customer-config?_t=" + Date.now() });
await new Promise(r => setTimeout(r, 4000));

// Check initial state
var r0 = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { var rp = window.__resizePersist; if (!rp) return 'NO RP'; var reg = rp.registry; return JSON.stringify({ prDefault: reg['vod-config-pr-summary'] ? reg['vod-config-pr-summary'].defaultHeight : 'N/A', prStyle: reg['vod-config-pr-summary'] ? reg['vod-config-pr-summary'].el.style.cssText : '' }); }" });
console.log("Initial:", JSON.stringify(r0));

// Expand PR
await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { var t = document.getElementById('pr-summary-toggle'); t.checked = true; t.dispatchEvent(new Event('change')); return 'ok'; }" });
await new Promise(r => setTimeout(r, 500));

var r1 = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { var c = document.querySelector('[data-resize-key=\"vod-config-pr-summary\"]'); var g = document.getElementById('vc-pr-detail-grid'); return JSON.stringify({ collapseH: c.offsetHeight, style: c.style.cssText, gridH: g.offsetHeight, hasContent: g.innerHTML.length > 0, gridRows: getComputedStyle(c).gridTemplateRows }); }" });
console.log("After expand:", JSON.stringify(r1));
