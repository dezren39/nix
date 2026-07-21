// Check grid state: row data, teams column, filter behavior
await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/irm/users" });
await new Promise(r => setTimeout(r, 3000));

// Check grid data and filter state
const r = await tools.mcp_chrome_devtools.evaluate_script({
  function: `(() => {
    const api = window.__currentGridApi;
    if (!api) return JSON.stringify({error: 'no gridApi'});
    
    // Get first 3 rows of data
    const rows = [];
    api.forEachNode((node, i) => { if (i < 3) rows.push(node.data); });
    
    // Check teams data on first row
    const first = rows[0] || {};
    
    // Check external filter
    const searchEl = document.getElementById('search-input');
    
    // Try buildColumnsMenu
    let colMenuCount = -1;
    try {
      window.buildColumnsMenu();
      colMenuCount = document.getElementById('columns-menu')?.children?.length || 0;
    } catch(e) { colMenuCount = 'error: ' + e.message; }
    
    // Get column defs count
    let colDefsCount = -1;
    try { colDefsCount = (api.getColumnDefs() || []).length; } catch(e) { colDefsCount = 'error: ' + e.message; }
    
    // Get column state count  
    let colStateCount = -1;
    try { colStateCount = (api.getColumnState() || []).length; } catch(e) { colStateCount = 'error: ' + e.message; }
    
    // Check if external filter is actually registered
    let filterPresent = 'unknown';
    try { filterPresent = api.isExternalFilterPresent ? api.isExternalFilterPresent() : 'no method'; } catch(e) { filterPresent = 'error: ' + e.message; }
    
    // Count visible vs total rows
    let totalRows = 0;
    let visibleRows = 0;
    api.forEachNode(() => totalRows++);
    api.forEachNodeAfterFilter(() => visibleRows++);
    
    return JSON.stringify({
      totalRows,
      visibleRows,
      searchValue: searchEl?.value || '',
      filterPresent,
      colDefsCount,
      colStateCount,
      colMenuCount,
      firstRow_team_ids: first.team_ids,
      firstRow_resolved_teams: first._resolved_teams,
      firstRow_display_name: first.display_name,
      firstRow_source: first._source,
    }, null, 2);
  })()`
});
console.log("=== Grid State ===");
console.log(r?.content?.[0]?.text || JSON.stringify(r));
