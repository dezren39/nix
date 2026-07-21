const r = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { var scripts = []; document.querySelectorAll('script[src]').forEach(function(s) { scripts.push(s.src.replace('http://localhost:8080/', '/')); }); return JSON.stringify(scripts); }" });
console.log(r.content[0].text);
