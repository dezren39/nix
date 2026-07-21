// Simulate SSE navigation: go to teams page, then back to users via sidebar
await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/irm/users" });
await new Promise(r => setTimeout(r, 2000));

// Click on Teams in sidebar to trigger SSE nav
await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    // Find and click the Teams sidebar link
    var links = document.querySelectorAll('a[href*="/irm/teams"]');
    for (var i = 0; i < links.length; i++) {
      if (links[i].closest('nav')) {
        links[i].click();
        return 'clicked teams link';
      }
    }
    return 'no teams link found in nav';
  }`
});
await new Promise(r => setTimeout(r, 2000));

// Now click Users in sidebar to go back
await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    var links = document.querySelectorAll('a[href*="/irm/users"]');
    for (var i = 0; i < links.length; i++) {
      if (links[i].closest('nav')) {
        links[i].click();
        return 'clicked users link';
      }
    }
    return 'no users link found in nav';
  }`
});
await new Promise(r => setTimeout(r, 3000));

// Now check if everything still works after SSE nav
const r = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    var api = window.__currentGridApi;
    if (!api) return {error: 'no gridApi after SSE nav'};
    
    var total = 0;
    api.forEachNode(function() { total++; });
    
    var rows = [];
    api.forEachNode(function(node, i) { if (i < 2) rows.push({ name: node.data.display_name, teams: node.data._resolved_teams }); });
    
    // Check search works
    var searchFn = typeof window.onUsersSearchChanged;
    var createFilter = typeof window.createSearchFilter;
    var buildMenu = typeof window.buildColumnsMenu;
    
    // Try building columns menu
    var menuCount = -1;
    try { window.buildColumnsMenu(); menuCount = document.getElementById('columns-menu').children.length; } catch(e) { menuCount = 'error: ' + e.message; }
    
    return { 
      totalRows: total, 
      sampleRows: rows, 
      searchFn: searchFn, 
      createFilter: createFilter, 
      buildMenu: buildMenu,
      menuCount: menuCount,
      pageTitle: document.title
    };
  }`
});
console.log("=== After SSE nav round-trip ===");
console.log(JSON.stringify(r, null, 2));
