// Hard reload to bypass cache
await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { location.reload(true); return 'reloading'; }" });
await new Promise(r => setTimeout(r, 5000));

// Check if resize-persist is the updated version
var r0 = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { var rp = window.__resizePersist; return rp ? JSON.stringify({ keys: Object.keys(rp.registry), prDefault: rp.registry['vod-config-pr-summary'] ? rp.registry['vod-config-pr-summary'].defaultHeight : 'N/A' }) : 'NO RP'; }" });
console.log("RP check:", JSON.stringify(r0));

// Expand
await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { var t = document.getElementById('pr-summary-toggle'); t.checked = true; t.dispatchEvent(new Event('change')); return 'ok'; }" });
await new Promise(r => setTimeout(r, 500));

var r = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { var c = document.querySelector('[data-resize-key=\"vod-config-pr-summary\"]'); return JSON.stringify({ h: c.offsetHeight, style: c.style.cssText }); }" });
console.log("After:", JSON.stringify(r));
