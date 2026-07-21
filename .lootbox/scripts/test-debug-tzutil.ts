// Check if tz-util.js loaded - check all scripts and window.TzUtil
const result = await tools.mcp_chrome_devtools.evaluate_script({
  "function": `function() {
    const scripts = Array.from(document.querySelectorAll('script[src]')).map(s => s.src);
    const tzScripts = scripts.filter(s => s.includes('tz-util'));
    
    return {
      allScripts: scripts,
      tzScripts: tzScripts,
      hasTzUtil: typeof window.TzUtil !== 'undefined',
      windowKeys: Object.keys(window).filter(k => k.toLowerCase().includes('tz') || k.toLowerCase().includes('util')).slice(0, 10),
      docReady: document.readyState
    };
  }`
});
console.log(JSON.stringify(result, null, 2));
