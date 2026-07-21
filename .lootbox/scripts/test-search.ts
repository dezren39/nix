// Type in search box and check if filter works
await new Promise(r => setTimeout(r, 1000));

// Type "Alex" in the search input
await tools.mcp_chrome_devtools.fill({ uid: "14_42", value: "Alex" });
await new Promise(r => setTimeout(r, 500));

// Trigger the oninput manually since fill might not fire it
await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    var el = document.getElementById('search-input');
    if (el) {
      el.value = 'Alex';
      el.dispatchEvent(new Event('input', { bubbles: true }));
    }
    return 'dispatched';
  }`
});
await new Promise(r => setTimeout(r, 1000));

// Check filter result
const r = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    var api = window.__currentGridApi;
    if (!api) return {error: 'no api'};
    var total = 0, visible = 0;
    api.forEachNode(function() { total++; });
    api.forEachNodeAfterFilter(function() { visible++; });
    var searchVal = (document.getElementById('search-input') || {}).value;
    return { total: total, visible: visible, searchVal: searchVal };
  }`
});
console.log("After typing 'Alex':", JSON.stringify(r, null, 2));

// Now clear and check
await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    var el = document.getElementById('search-input');
    if (el) {
      el.value = '';
      el.dispatchEvent(new Event('input', { bubbles: true }));
    }
    return 'cleared';
  }`
});
await new Promise(r => setTimeout(r, 500));

const r2 = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    var api = window.__currentGridApi;
    if (!api) return {error: 'no api'};
    var total = 0, visible = 0;
    api.forEachNode(function() { total++; });
    api.forEachNodeAfterFilter(function() { visible++; });
    return { total: total, visible: visible };
  }`
});
console.log("After clearing:", JSON.stringify(r2, null, 2));
