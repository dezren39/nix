var r = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { return fetch('/resize-persist.js?_=' + Date.now()).then(function(r){return r.text()}).then(function(t){ return t; }); }" });
// save to file for reading
require('fs').writeFileSync('/tmp/resize-persist-served.js', r.content[0].text.replace(/^Script ran on page and returned:\n```[^\n]*\n/, '').replace(/\n```$/, ''));
