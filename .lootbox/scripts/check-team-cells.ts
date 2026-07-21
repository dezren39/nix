// Check what the teams cells actually render
await new Promise(r => setTimeout(r, 1000));

const r = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    // Find all cells in the team_ids column
    var teamCells = document.querySelectorAll('[col-id="team_ids"]');
    var results = [];
    for (var i = 0; i < Math.min(teamCells.length, 5); i++) {
      results.push({
        html: teamCells[i].innerHTML.substring(0, 200),
        text: teamCells[i].textContent.substring(0, 100)
      });
    }
    
    // Also check the actual row data for team_ids
    var api = window.__currentGridApi;
    var rowTeams = [];
    if (api) {
      api.forEachNode(function(node, i) {
        if (i < 5) {
          rowTeams.push({
            name: node.data.display_name,
            team_ids: node.data.team_ids,
            resolved: node.data._resolved_teams
          });
        }
      });
    }
    
    return { cellsFound: teamCells.length, cells: results, rowData: rowTeams };
  }`
});
console.log(JSON.stringify(r, null, 2));
