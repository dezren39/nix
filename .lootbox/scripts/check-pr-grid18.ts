const r = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { var hasComponent = typeof window.PrSummaryGrid !== 'undefined'; var scriptTags = []; document.querySelectorAll('script[src]').forEach(function(s) { if (s.src.indexOf('pr-summary') > -1) scriptTags.push(s.src); }); var consoleErrs = []; return JSON.stringify({ hasComponent: hasComponent, scripts: scriptTags, agGridLoaded: typeof agGrid !== 'undefined' }); }" });
console.log(JSON.stringify(r, null, 2));

// Check console for errors
const msgs = await tools.mcp_chrome_devtools.list_console_messages({ level: "error" });
console.log("Errors:", JSON.stringify(msgs, null, 2).substring(0, 2000));
