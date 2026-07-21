// Check what URL we're actually on and what scripts are loaded
await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/support-actions/vod-config" });
await new Promise(r => setTimeout(r, 3000));

const result = await tools.mcp_chrome_devtools.evaluate_script({
  "function": `function() {
    var scripts = document.querySelectorAll('script[src]');
    var srcs = [];
    scripts.forEach(function(s) { srcs.push(s.src); });
    return {
      url: window.location.href,
      title: document.title,
      scriptCount: scripts.length,
      scripts: srcs.filter(function(s) { return s.indexOf('ag-grid') !== -1 || s.indexOf('events-grid') !== -1 || s.indexOf('support-actions') !== -1; }),
      mainContent: document.querySelector('main') ? document.querySelector('main').innerHTML.substring(0, 300) : 'no main',
    };
  }`
});
console.log(JSON.stringify(result, null, 2));
