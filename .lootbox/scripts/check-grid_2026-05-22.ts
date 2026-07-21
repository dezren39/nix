// Navigate to VOD config page and check the events grid state
await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/support-actions/vod-config" });
await new Promise(r => setTimeout(r, 3000));

// Check if grid exists and has filter icons
const result = await tools.mcp_chrome_devtools.evaluate_script({
  expression: `(function() {
    var grid = document.querySelector('#events-grid');
    if (!grid) return { error: 'no grid element' };
    var api = window.__vc_events_grid && window.__vc_events_grid.gridApi;
    if (!api) return { error: 'no grid api' };
    var defs = api.getColumnDefs();
    var state = api.getColumnState();
    var filterBtns = grid.querySelectorAll('.ag-header-cell-menu-button');
    var headerCells = grid.querySelectorAll('.ag-header-cell');
    return {
      columnDefs: defs ? defs.length : null,
      columnDefsDetail: defs ? defs.map(d => ({ field: d.field, headerName: d.headerName, filter: d.filter })) : null,
      columnState: state ? state.length : null,
      filterButtons: filterBtns.length,
      headerCells: headerCells.length,
      gridClasses: grid.className,
    };
  })()`
});
console.log(JSON.stringify(result, null, 2));
