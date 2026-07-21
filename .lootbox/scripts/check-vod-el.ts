const result = await tools.mcp_chrome_devtools.evaluate_script({
  expression: `JSON.stringify({
    el: document.getElementById('admin-last-refresh')?.textContent,
    tz: !!window.TzUtil,
    formatted: window.TzUtil?.formatDate('2026-04-22T01:00:00Z')
  })`
});
console.log(result);
