await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/support-actions/edit-vod-customer-config?_t=" + Date.now() });
await new Promise(r => setTimeout(r, 4000));

// Check if the JS file has our fix
var r0 = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { return fetch('/static/js/resize-persist.js?_=' + Date.now()).then(function(r){return r.text()}).then(function(t){ var idx = t.indexOf('Expanding'); return t.substring(idx, idx + 150); }); }" });
console.log("JS source:", JSON.stringify(r0));

// Check console errors
var msgs = await tools.mcp_chrome_devtools.list_console_messages({});
console.log("Console:", JSON.stringify(msgs).substring(0, 1000));
