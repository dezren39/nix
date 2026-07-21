// Take screenshot and check teams column + search visuals
await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/irm/users" });
await new Promise(r => setTimeout(r, 3000));

// Take screenshot
const screenshot = await tools.mcp_chrome_devtools.take_screenshot({});
console.log("Screenshot taken");

// Now check teams column visibility - is it hidden?
const r = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    var api = window.__currentGridApi;
    if (!api) return {error: 'no api'};
    var state = api.getColumnState() || [];
    var teamCol = state.find(function(s) { return s.colId === 'team_ids'; });
    var teamsVisible = teamCol ? !teamCol.hide : 'not found';
    
    // Check all column visibility
    var visibleCols = state.filter(function(s) { return !s.hide; }).map(function(s) { return s.colId; });
    
    // Also check if saved column state in localStorage might be hiding teams
    var savedState = null;
    try { savedState = localStorage.getItem('op-users-columns'); } catch(e) {}
    var savedParsed = null;
    if (savedState) {
      try { 
        savedParsed = JSON.parse(savedState);
        var savedTeamCol = savedParsed.find(function(s) { return s.colId === 'team_ids'; });
        var savedTeamsVisible = savedTeamCol ? !savedTeamCol.hide : 'not found in saved';
      } catch(e) {}
    }
    
    return {
      teamsVisible: teamsVisible,
      visibleCols: visibleCols,
      hasSavedState: !!savedState,
      savedTeamsVisible: savedTeamsVisible || 'n/a'
    };
  }`
});
console.log("=== Column visibility ===");
console.log(JSON.stringify(r, null, 2));
