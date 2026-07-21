// Fetch the actual JS file to see if it has our fix
var r = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { return fetch('/static/js/resize-persist.js').then(function(r){return r.text()}).then(function(t){ return t.indexOf('registry[key].defaultHeight') > -1 ? 'HAS FIX' : 'OLD VERSION: ' + t.substring(t.indexOf('Expanding'), t.indexOf('Expanding') + 100); }); }" });
console.log(JSON.stringify(r));
