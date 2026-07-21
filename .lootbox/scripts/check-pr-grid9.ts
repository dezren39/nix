// Check what static path the server uses
var r = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { var scripts = document.querySelectorAll('script[src]'); var srcs = []; scripts.forEach(function(s) { if (s.src.indexOf('resize') > -1 || s.src.indexOf('grid-utils') > -1) srcs.push(s.src); }); return JSON.stringify(srcs); }" });
console.log("Scripts:", JSON.stringify(r));
