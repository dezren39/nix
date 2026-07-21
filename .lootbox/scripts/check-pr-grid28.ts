const r = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { return fetch('/api/support-actions/vod-config/events?all=true').then(function(r) { return r.text(); }).then(function(t) { return t.substring(0, 500); }); }" });
console.log(r.content[0].text);
