// Test collapse then expand
var r = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { var key = 'vod-config-pr-summary'; var reg = window.__resizePersist.registry[key]; var el = reg.el; var cb = el.querySelector(':scope > input[type=checkbox]'); cb.checked = false; cb.dispatchEvent(new Event('change', {bubbles:true})); return JSON.stringify({ collapsed: { checked: cb.checked, style: el.style.height } }); }" });
console.log("Collapsed:", JSON.stringify(r));

await new Promise(r => setTimeout(r, 300));

var r2 = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { var key = 'vod-config-pr-summary'; var reg = window.__resizePersist.registry[key]; var el = reg.el; var cb = el.querySelector(':scope > input[type=checkbox]'); cb.checked = true; cb.dispatchEvent(new Event('change', {bubbles:true})); return JSON.stringify({ expanded: { checked: cb.checked, style: el.style.height, elH: el.offsetHeight } }); }" });
console.log("Expanded:", JSON.stringify(r2));
