// Hard refresh and test TzUtil
await tools.mcp_chrome_devtools.navigate_page({ url: "about:blank" });
await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/irm/settings" });

// Wait a moment for page to fully load
await new Promise(r => setTimeout(r, 2000));

// Check console for errors first
const console_msgs = await tools.mcp_chrome_devtools.list_console_messages({});
console.log("=== Console messages ===");
console.log(JSON.stringify(console_msgs, null, 2));

// Check TzUtil
const result = await tools.mcp_chrome_devtools.evaluate_script({
  "function": `function() {
    return {
      hasTzUtil: typeof window.TzUtil !== 'undefined',
      tzUtilKeys: typeof window.TzUtil !== 'undefined' ? Object.keys(window.TzUtil) : [],
      scripts: Array.from(document.querySelectorAll('script[src]'))
        .filter(s => s.src.includes('tz-util'))
        .map(s => s.src),
      docReady: document.readyState
    };
  }`
});
console.log("\n=== TzUtil check ===");
console.log(JSON.stringify(result, null, 2));
