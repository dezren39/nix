var r = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { return fetch('/resize-persist.js?_=' + Date.now()).then(function(r){return r.text()}).then(function(t){ var idx = t.indexOf('Expanding'); if (idx < 0) return 'NO EXPANDING FOUND, len=' + t.length; return t.substring(idx, idx + 200); }); }" });
console.log("Source:", JSON.stringify(r));
