// Test timezone search functionality
const result = await tools.mcp_chrome_devtools.evaluate_script({
  "function": `function() {
    const tz = window.TzUtil;
    if (!tz) return { error: 'TzUtil not found' };
    
    const nySearch = tz.searchTimezones('new york').slice(0, 3);
    const offsetSearch = tz.searchTimezones('-5').slice(0, 3);
    const estSearch = tz.searchTimezones('EST').slice(0, 3);
    const indiaSearch = tz.searchTimezones('india').slice(0, 3);
    
    // Check first result structure
    const firstResult = nySearch[0] || null;
    
    return {
      firstResultKeys: firstResult ? Object.keys(firstResult) : [],
      newYork: nySearch,
      offset5: offsetSearch,
      est: estSearch,
      india: indiaSearch,
      totalTimezones: tz.getTimezoneList().length
    };
  }`
});
console.log("=== TzUtil search test ===");
console.log(JSON.stringify(result, null, 2));
