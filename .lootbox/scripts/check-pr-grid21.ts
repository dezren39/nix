const r = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { var scripts = []; document.querySelectorAll('script[src]').forEach(function(s) { if (s.src.indexOf('grid-utils') > -1) scripts.push(s.src); }); return JSON.stringify(scripts); }" });
console.log(r.content[0].text);
