const r = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { var hasComponent = typeof window.PrSummaryGrid !== 'undefined'; var scriptTags: any[] = []; document.querySelectorAll('script[src]').forEach(function(s: any) { if (s.src.indexOf('pr-summary') > -1) scriptTags.push(s.src); }); return JSON.stringify({ hasComponent: hasComponent, scripts: scriptTags, agGridLoaded: typeof agGrid !== 'undefined' }); }" });
console.log(JSON.stringify(r, null, 2));

const msgs = await tools.mcp_chrome_devtools.list_console_messages({});
console.log("Messages:", JSON.stringify(msgs).substring(0, 2000));
