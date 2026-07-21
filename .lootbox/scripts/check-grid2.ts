// Check grid state
await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/irm/users" });
await new Promise(r => setTimeout(r, 3000));

const r = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    var api = window.__currentGridApi;
    if (!api) return {error: 'no gridApi'};
    
    var rows = [];
    api.forEachNode(function(node, i) { if (i < 3) rows.push(node.data); });
    var first = rows[0] || {};
    
    var colDefsCount = -1;
    try { colDefsCount = (api.getColumnDefs() || []).length; } catch(e) { colDefsCount = 'error: ' + e.message; }
    
    var colStateCount = -1;
    try { colStateCount = (api.getColumnState() || []).length; } catch(e) { colStateCount = 'error: ' + e.message; }
    
    var colMenuCount = -1;
    try { 
      window.buildColumnsMenu();
      colMenuCount = (document.getElementById('columns-menu') || {}).children?.length || 0;
    } catch(e) { colMenuCount = 'error: ' + e.message; }
    
    var totalRows = 0, visibleRows = 0;
    api.forEachNode(function() { totalRows++; });
    api.forEachNodeAfterFilter(function() { visibleRows++; });
    
    return {
      totalRows: totalRows,
      visibleRows: visibleRows,
      searchValue: (document.getElementById('search-input') || {}).value || '',
      colDefsCount: colDefsCount,
      colStateCount: colStateCount,
      colMenuCount: colMenuCount,
      firstRow_team_ids: first.team_ids,
      firstRow_resolved_teams: first._resolved_teams,
      firstRow_name: first.display_name,
      firstRow_source: first._source,
    };
  }`
});
console.log("=== Grid State ===");
console.log(JSON.stringify(r, null, 2));
