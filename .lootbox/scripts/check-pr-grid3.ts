// Force cache bypass
await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/support-actions/edit-vod-customer-config?_t=" + Date.now() });
await new Promise(r => setTimeout(r, 4000));

// Check what resize-persist captured as default height
var r1 = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { var rp = window.__resizePersist; if (!rp) return 'NO RESIZE PERSIST'; var reg = rp.registry; var keys = Object.keys(reg); var info = {}; keys.forEach(function(k) { info[k] = { defaultHeight: reg[k].defaultHeight, currentH: reg[k].el.offsetHeight, style: reg[k].el.style.cssText }; }); return JSON.stringify(info); }" });
console.log("Registry:", JSON.stringify(r1, null, 2));

// Now expand PR
var r2 = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { var toggle = document.getElementById('pr-summary-toggle'); if (!toggle) return 'no toggle'; toggle.checked = true; toggle.dispatchEvent(new Event('change')); return 'expanded'; }" });
console.log("Toggle:", JSON.stringify(r2));

await new Promise(r => setTimeout(r, 500));

var r3 = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { var collapse = document.querySelector('[data-resize-key=\"vod-config-pr-summary\"]'); var grid = document.getElementById('vc-pr-detail-grid'); return JSON.stringify({ collapseH: collapse ? collapse.offsetHeight : 0, style: collapse ? collapse.style.cssText : '', gridH: grid ? grid.offsetHeight : 0, hasContent: grid ? grid.innerHTML.length > 0 : false }); }" });
console.log("After expand:", JSON.stringify(r3, null, 2));
