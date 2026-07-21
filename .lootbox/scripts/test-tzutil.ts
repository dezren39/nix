// Test 3: Evaluate TzUtil in the browser to verify it loaded correctly
// First navigate to settings page where TzUtil is loaded
await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/irm/settings" });

const result = await tools.mcp_chrome_devtools.evaluate_script({
  "function": `function() {
      const tzUtil = window.TzUtil;
      if (!tzUtil) return { error: 'TzUtil not found on window' };
      
      const tz = tzUtil.getTimezone();
      const testDate = tzUtil.formatDate('2026-04-12T14:30:00Z');
      const searchResults = tzUtil.searchTimezones('new york').slice(0, 3).map(z => z.name);
      const offsetSearch = tzUtil.searchTimezones('-5').slice(0, 3).map(z => z.name);
      
      return {
        loaded: true,
        currentTimezone: tz,
        formattedTestDate: testDate,
        searchNewYork: searchResults,
        searchOffset5: offsetSearch,
        autoDetected: Intl.DateTimeFormat().resolvedOptions().timeZone,
      };
    }`
});
console.log("=== TzUtil browser evaluation ===");
console.log(JSON.stringify(result, null, 2));
